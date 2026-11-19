//! Forwarding of local API calls to the daemon.
//!
//! Every call opens an HTTP/2 connection to the endpoint of the daemon and
//! speaks the Connect protocol in binary form. The webview works with raw
//! message bytes, so this layer never interprets protobuf.

use std::io;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use http_body_util::{BodyExt, Full, LengthLimitError, Limited};
use hyper::body::{Bytes, Incoming};
use hyper::client::conn::http2;
use hyper::header::CONTENT_TYPE;
use hyper::{Method, Request, StatusCode};
use hyper_util::rt::{TokioExecutor, TokioIo};
use serde::Serialize;
use tokio::io::{AsyncRead, AsyncWrite, ReadBuf};

use crate::observability;

/// Content types of the Connect protocol in binary form.
const UNARY_CONTENT_TYPE: &str = "application/proto";
const STREAM_CONTENT_TYPE: &str = "application/connect+proto";

/// Limits of the exchange with the daemon.
const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_RESPONSE_BYTES: usize = 16 << 20;

/// ApiResponse is the result of a unary call. The body is base64 encoded so it
/// can travel through the IPC as JSON.
#[derive(Serialize)]
pub struct ApiResponse {
    pub status: u16,
    pub body: String,
}

/// EndpointStream is the connection to the endpoint of the daemon.
pub enum EndpointStream {
    #[cfg(unix)]
    Unix(tokio::net::UnixStream),
    #[cfg(windows)]
    Pipe(tokio::net::windows::named_pipe::NamedPipeClient),
}

impl AsyncRead for EndpointStream {
    fn poll_read(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &mut ReadBuf<'_>,
    ) -> Poll<io::Result<()>> {
        match &mut *self {
            #[cfg(unix)]
            EndpointStream::Unix(stream) => Pin::new(stream).poll_read(cx, buf),
            #[cfg(windows)]
            EndpointStream::Pipe(pipe) => Pin::new(pipe).poll_read(cx, buf),
        }
    }
}

impl AsyncWrite for EndpointStream {
    fn poll_write(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &[u8],
    ) -> Poll<io::Result<usize>> {
        match &mut *self {
            #[cfg(unix)]
            EndpointStream::Unix(stream) => Pin::new(stream).poll_write(cx, buf),
            #[cfg(windows)]
            EndpointStream::Pipe(pipe) => Pin::new(pipe).poll_write(cx, buf),
        }
    }

    fn poll_flush(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<io::Result<()>> {
        match &mut *self {
            #[cfg(unix)]
            EndpointStream::Unix(stream) => Pin::new(stream).poll_flush(cx),
            #[cfg(windows)]
            EndpointStream::Pipe(pipe) => Pin::new(pipe).poll_flush(cx),
        }
    }

    fn poll_shutdown(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<io::Result<()>> {
        match &mut *self {
            #[cfg(unix)]
            EndpointStream::Unix(stream) => Pin::new(stream).poll_shutdown(cx),
            #[cfg(windows)]
            EndpointStream::Pipe(pipe) => Pin::new(pipe).poll_shutdown(cx),
        }
    }
}

/// Connect opens the endpoint of the daemon. A unix socket that another user
/// owns, that other users can reach or that other users can replace is refused
/// before anything is sent to it, the same way the clients of the daemon do.
pub async fn connect(endpoint: &str) -> io::Result<EndpointStream> {
    #[cfg(unix)]
    {
        check_endpoint(std::path::Path::new(endpoint))?;
        tokio::net::UnixStream::connect(endpoint)
            .await
            .map(EndpointStream::Unix)
    }
    #[cfg(windows)]
    {
        open_pipe(endpoint).await.map(EndpointStream::Pipe)
    }
}

/// How long a client waits for a free instance of the named pipe.
#[cfg(windows)]
const PIPE_BUSY_LIMIT: Duration = Duration::from_secs(5);

/// How often a client asks again for a free instance of the named pipe.
#[cfg(windows)]
const PIPE_BUSY_INTERVAL: Duration = Duration::from_millis(20);

/// open_pipe opens the named pipe of the daemon. The daemon keeps one free
/// instance of the pipe and creates the next one after a client takes it, so
/// clients that arrive at the same moment find every instance busy for a short
/// while. They ask again until an instance is free.
#[cfg(windows)]
async fn open_pipe(endpoint: &str) -> io::Result<tokio::net::windows::named_pipe::NamedPipeClient> {
    const ERROR_PIPE_BUSY: i32 = 231;

    let deadline = tokio::time::Instant::now() + PIPE_BUSY_LIMIT;
    loop {
        match tokio::net::windows::named_pipe::ClientOptions::new().open(endpoint) {
            Err(err)
                if err.raw_os_error() == Some(ERROR_PIPE_BUSY)
                    && tokio::time::Instant::now() < deadline =>
            {
                tokio::time::sleep(PIPE_BUSY_INTERVAL).await;
            }
            result => return result,
        }
    }
}

/// transport_error reports the category of a failed exchange with the daemon
/// and returns its message, which keeps the details for the window. The
/// category stays free of paths and addresses.
fn transport_error(category: &str, message: impl Into<String>) -> String {
    observability::capture_error(category);
    message.into()
}

/// check_endpoint verifies that the endpoint is a socket of the current user
/// that only the owner can reach, in a directory other users cannot write to.
#[cfg(unix)]
fn check_endpoint(path: &std::path::Path) -> io::Result<()> {
    use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};

    let insecure = || {
        io::Error::new(
            io::ErrorKind::PermissionDenied,
            "the endpoint is reachable by other users",
        )
    };
    let dir = match path.parent() {
        Some(dir) if !dir.as_os_str().is_empty() => dir,
        _ => std::path::Path::new("."),
    };
    if std::fs::metadata(dir)?.permissions().mode() & 0o022 != 0 {
        return Err(insecure());
    }
    let info = std::fs::symlink_metadata(path)?;
    if !info.file_type().is_socket() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "the endpoint is not a socket",
        ));
    }
    // SAFETY: getuid has no preconditions and cannot fail.
    let uid = unsafe { libc::getuid() };
    if info.mode() & 0o077 != 0 || info.uid() != uid {
        return Err(insecure());
    }
    Ok(())
}

/// Call performs a unary call of the service.
pub async fn call(
    endpoint: &str,
    service: &str,
    method: &str,
    body: &[u8],
) -> Result<ApiResponse, String> {
    call_within(endpoint, service, method, body, REQUEST_TIMEOUT).await
}

/// call_within performs a unary call that has limit for the response head and
/// limit again for the response body.
async fn call_within(
    endpoint: &str,
    service: &str,
    method: &str,
    body: &[u8],
    limit: Duration,
) -> Result<ApiResponse, String> {
    let request = Bytes::copy_from_slice(body);
    let (response, _sender) = open(
        endpoint,
        service,
        method,
        UNARY_CONTENT_TYPE,
        request,
        Some(limit),
    )
    .await?;
    let status = response.status();
    let body = tokio::time::timeout(limit, read_body(response.into_body(), MAX_RESPONSE_BYTES))
        .await
        .map_err(|_| {
            transport_error(
                "the daemon did not finish the response in time",
                "the daemon did not finish the response in time",
            )
        })??;
    Ok(ApiResponse {
        status: status.as_u16(),
        body: BASE64.encode(body),
    })
}

/// Stream performs a server streaming call of the service and hands every
/// chunk of the response body to on_chunk. The request body already carries the
/// framing of the Connect protocol and the chunks of the response keep it, so
/// this layer never parses either of them.
pub async fn stream(
    endpoint: &str,
    service: &str,
    method: &str,
    body: &[u8],
    mut on_chunk: impl FnMut(&[u8]) -> Result<(), String>,
) -> Result<(), String> {
    // The daemon sends the response head of a stream with the first message,
    // which may take as long as the next event.
    let request = Bytes::copy_from_slice(body);
    let (response, _sender) = open(
        endpoint,
        service,
        method,
        STREAM_CONTENT_TYPE,
        request,
        None,
    )
    .await?;
    if response.status() != StatusCode::OK {
        let status = response.status();
        let body = read_body(response.into_body(), MAX_RESPONSE_BYTES).await?;
        return Err(format!(
            "the daemon answered with {status}: {}",
            String::from_utf8_lossy(&body)
        ));
    }

    let mut body = response.into_body();
    while let Some(frame) = body.frame().await {
        let frame = frame.map_err(|err| {
            transport_error(
                "reading the event stream failed",
                format!("reading the event stream failed: {err}"),
            )
        })?;
        if let Ok(data) = frame.into_data()
            && !data.is_empty()
        {
            on_chunk(&data)?;
        }
    }
    Ok(())
}

/// open starts a request of the service and returns the response head. The
/// sender is kept by the caller so the connection lives until the body is read.
async fn open(
    endpoint: &str,
    service: &str,
    method: &str,
    content_type: &str,
    body: Bytes,
    timeout: Option<Duration>,
) -> Result<(hyper::Response<Incoming>, http2::SendRequest<Full<Bytes>>), String> {
    let uri = procedure(service, method)?;
    let stream = connect(endpoint).await.map_err(|err| {
        transport_error(
            "cannot reach the daemon",
            format!("cannot reach the daemon: {err}"),
        )
    })?;
    let (mut sender, connection) = http2::handshake(TokioExecutor::new(), TokioIo::new(stream))
        .await
        .map_err(|err| {
            transport_error(
                "cannot open a session with the daemon",
                format!("cannot open a session with the daemon: {err}"),
            )
        })?;
    tokio::spawn(async move {
        let _ = connection.await;
    });

    let request = Request::builder()
        .method(Method::POST)
        .uri(uri)
        .header(CONTENT_TYPE, content_type)
        .header("connect-protocol-version", "1")
        .body(Full::new(body))
        .map_err(|err| format!("cannot build the request: {err}"))?;

    let pending = sender.send_request(request);
    let response = match timeout {
        Some(limit) => tokio::time::timeout(limit, pending).await.map_err(|_| {
            transport_error(
                "the daemon did not answer in time",
                "the daemon did not answer in time",
            )
        })?,
        None => pending.await,
    }
    .map_err(|err| {
        transport_error(
            "the request to the daemon failed",
            format!("the request to the daemon failed: {err}"),
        )
    })?;
    Ok((response, sender))
}

/// procedure returns the request URI of a method and refuses names that do not
/// look like a protobuf service and method.
fn procedure(service: &str, method: &str) -> Result<String, String> {
    let service_ok = !service.is_empty()
        && service
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '.' || c == '_');
    let method_ok = !method.is_empty()
        && method
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '_');
    if !service_ok || !method_ok {
        return Err("the procedure name is not valid".to_string());
    }
    Ok(format!("http://unison/{service}/{method}"))
}

/// read_body collects a response body up to limit bytes. The limit applies
/// while the body arrives, so a larger response is never held in memory.
async fn read_body(body: Incoming, limit: usize) -> Result<Vec<u8>, String> {
    let collected = Limited::new(body, limit)
        .collect()
        .await
        .map_err(|err| {
            if err.is::<LengthLimitError>() {
                transport_error(
                    "the response is larger than the limit",
                    format!("the response is larger than {limit} bytes"),
                )
            } else {
                transport_error(
                    "reading the response failed",
                    format!("reading the response failed: {err}"),
                )
            }
        })?
        .to_bytes();
    Ok(collected.to_vec())
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use crate::testutil::{TempDir, bind_socket, block_on, set_mode};
    use http_body_util::combinators::BoxBody;
    use hyper::body::{Body, Frame};
    use hyper::server::conn::http2 as server;
    use hyper::service::service_fn;
    use std::convert::Infallible;
    use std::path::Path;
    use std::sync::{Arc, Mutex};
    use tokio::sync::oneshot;

    type Reply = hyper::Response<BoxBody<Bytes, Infallible>>;

    /// Seen is what the test server received.
    #[derive(Default, Clone, Debug)]
    struct Seen {
        path: String,
        content_type: String,
        protocol: String,
        body: Vec<u8>,
    }

    /// Server answers every request of one connection with reply, records the
    /// request and reports when the connection ends.
    struct Server {
        seen: Arc<Mutex<Seen>>,
        closed: oneshot::Receiver<()>,
    }

    fn full(status: StatusCode, body: impl Into<Bytes>) -> Reply {
        hyper::Response::builder()
            .status(status)
            .body(Full::new(body.into()).boxed())
            .expect("build the response")
    }

    /// serve accepts one connection on path and answers it.
    fn serve(path: &Path, reply: fn() -> Reply) -> Server {
        let listener = bind_socket(path);
        listener.set_nonblocking(true).expect("nonblocking");
        let listener = tokio::net::UnixListener::from_std(listener).expect("listener");
        let seen = Arc::new(Mutex::new(Seen::default()));
        let (done, closed) = oneshot::channel();
        let record = seen.clone();
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.expect("accept");
            let service = service_fn(move |request: Request<Incoming>| {
                let record = record.clone();
                async move {
                    let header = |name: &str| {
                        request
                            .headers()
                            .get(name)
                            .and_then(|value| value.to_str().ok())
                            .unwrap_or_default()
                            .to_string()
                    };
                    let mut seen = Seen {
                        path: request.uri().path().to_string(),
                        content_type: header("content-type"),
                        protocol: header("connect-protocol-version"),
                        body: Vec::new(),
                    };
                    seen.body = request
                        .into_body()
                        .collect()
                        .await
                        .unwrap()
                        .to_bytes()
                        .to_vec();
                    *record.lock().unwrap() = seen;
                    Ok::<_, Infallible>(reply())
                }
            });
            let _ = server::Builder::new(TokioExecutor::new())
                .serve_connection(TokioIo::new(stream), service)
                .await;
            let _ = done.send(());
        });
        Server { seen, closed }
    }

    /// Endless is a response body that never ends, like the event stream of a
    /// daemon without events.
    struct Endless;

    impl Body for Endless {
        type Data = Bytes;
        type Error = Infallible;

        fn poll_frame(
            self: Pin<&mut Self>,
            _cx: &mut Context<'_>,
        ) -> Poll<Option<Result<Frame<Bytes>, Infallible>>> {
            Poll::Pending
        }
    }

    #[test]
    fn procedure_accepts_protobuf_names() {
        assert_eq!(
            procedure("unison.local.v1.UnisonService", "GetStatus").as_deref(),
            Ok("http://unison/unison.local.v1.UnisonService/GetStatus")
        );
        assert!(procedure("a_b.C9", "Do_It").is_ok());
    }

    #[test]
    fn procedure_refuses_other_names() {
        for (service, method) in [
            ("", "GetStatus"),
            ("unison.Service", ""),
            ("unison/Service", "GetStatus"),
            ("unison.Service", "Get/Status"),
            ("..", "x?y=1"),
            ("unison.Service", "Get Status"),
            ("unison.Service", "GetStatus#x"),
            ("сервис", "GetStatus"),
        ] {
            assert!(
                procedure(service, method).is_err(),
                "{service}/{method} was accepted"
            );
        }
    }

    #[test]
    fn call_forwards_the_request_and_returns_the_response() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let server = serve(&path, || full(StatusCode::OK, &b"\x01\x02reply"[..]));
            let response = call(path.to_str().unwrap(), "unison.S", "Get", b"\x08\x01")
                .await
                .expect("call");
            assert_eq!(response.status, 200);
            assert_eq!(BASE64.decode(response.body).unwrap(), b"\x01\x02reply");

            let seen = server.seen.lock().unwrap().clone();
            assert_eq!(seen.path, "/unison.S/Get");
            assert_eq!(seen.content_type, UNARY_CONTENT_TYPE);
            assert_eq!(seen.protocol, "1");
            assert_eq!(seen.body, b"\x08\x01");
        });
    }

    #[test]
    fn call_returns_the_status_of_an_error() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let _server = serve(&path, || {
                full(StatusCode::NOT_FOUND, r#"{"code":"not_found"}"#)
            });
            let response = call(path.to_str().unwrap(), "unison.S", "Get", b"")
                .await
                .expect("call");
            assert_eq!(response.status, 404);
            assert_eq!(
                BASE64.decode(response.body).unwrap(),
                br#"{"code":"not_found"}"#
            );
        });
    }

    #[test]
    fn call_refuses_a_response_above_the_limit() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let _server = serve(&path, || {
                full(StatusCode::OK, vec![0u8; MAX_RESPONSE_BYTES + 1])
            });
            let err = call(path.to_str().unwrap(), "unison.S", "Get", b"")
                .await
                .err()
                .expect("an oversized response was accepted");
            assert!(err.contains("larger than"), "unexpected error: {err}");
        });
    }

    #[test]
    fn call_reports_a_missing_daemon() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let err = block_on(call(path.to_str().unwrap(), "unison.S", "Get", b""))
            .err()
            .expect("a call without a daemon succeeded");
        assert!(err.starts_with("cannot reach the daemon"), "{err}");
    }

    #[test]
    fn stream_hands_over_the_chunks() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let server = serve(&path, || {
                full(StatusCode::OK, &b"\x00\x00\x00\x00\x01e"[..])
            });
            let mut chunks = Vec::new();
            stream(
                path.to_str().unwrap(),
                "unison.S",
                "Watch",
                b"req",
                |data| {
                    chunks.extend_from_slice(data);
                    Ok(())
                },
            )
            .await
            .expect("stream");
            assert_eq!(chunks, b"\x00\x00\x00\x00\x01e");
            let seen = server.seen.lock().unwrap().clone();
            assert_eq!(seen.content_type, STREAM_CONTENT_TYPE);
            assert_eq!(seen.body, b"req");
        });
    }

    #[test]
    fn stream_reports_an_error_status() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let _server = serve(&path, || full(StatusCode::SERVICE_UNAVAILABLE, "busy"));
            let err = stream(path.to_str().unwrap(), "unison.S", "Watch", b"", |_| {
                panic!("a chunk of a failed stream was delivered")
            })
            .await
            .expect_err("a failed stream succeeded");
            assert!(err.contains("503") && err.contains("busy"), "{err}");
        });
    }

    #[test]
    fn stream_stops_when_the_chunk_cannot_be_delivered() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let _server = serve(&path, || full(StatusCode::OK, "chunk"));
            let err = stream(path.to_str().unwrap(), "unison.S", "Watch", b"", |_| {
                Err("the webview is gone".to_string())
            })
            .await
            .expect_err("the stream ignored the failed delivery");
            assert_eq!(err, "the webview is gone");
        });
    }

    #[test]
    fn cancelled_stream_closes_its_connection() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            let server = serve(&path, || hyper::Response::new(BodyExt::boxed(Endless)));
            let streams = Arc::new(crate::streams::Streams::default());
            let running = streams.open("main", 7);

            let canceller = streams.clone();
            tokio::spawn(async move {
                tokio::time::sleep(Duration::from_millis(200)).await;
                assert!(canceller.cancel("main", 7), "the stream was not open");
            });

            let endpoint = path.to_str().unwrap();
            let result = tokio::time::timeout(
                Duration::from_secs(5),
                running.run(stream(endpoint, "unison.S", "Watch", b"", |_| Ok(()))),
            )
            .await
            .expect("the cancelled stream kept running");
            assert_eq!(result, Ok(()));

            tokio::time::timeout(Duration::from_secs(5), server.closed)
                .await
                .expect("the daemon still holds the connection of a cancelled stream")
                .expect("the server stopped");
        });
    }

    /// serve_with answers every request of one connection with handler.
    fn serve_with<F, Fut>(path: &Path, handler: F)
    where
        F: Fn() -> Fut + Clone + Send + 'static,
        Fut: std::future::Future<Output = hyper::Response<BoxBody<Bytes, io::Error>>>
            + Send
            + 'static,
    {
        let listener = bind_socket(path);
        listener.set_nonblocking(true).expect("nonblocking");
        let listener = tokio::net::UnixListener::from_std(listener).expect("listener");
        tokio::spawn(async move {
            let (stream, _) = listener.accept().await.expect("accept");
            let service = service_fn(move |_request: Request<Incoming>| {
                let handler = handler.clone();
                async move { Ok::<_, Infallible>(handler().await) }
            });
            let _ = server::Builder::new(TokioExecutor::new())
                .serve_connection(TokioIo::new(stream), service)
                .await;
        });
    }

    /// Broken is a response body that sends one chunk and fails a moment
    /// later, like a daemon that stops in the middle of a stream.
    struct Broken {
        sent: bool,
        pause: Pin<Box<tokio::time::Sleep>>,
    }

    impl Broken {
        fn new() -> Self {
            Self {
                sent: false,
                pause: Box::pin(tokio::time::sleep(Duration::from_millis(100))),
            }
        }
    }

    impl Body for Broken {
        type Data = Bytes;
        type Error = io::Error;

        fn poll_frame(
            mut self: Pin<&mut Self>,
            cx: &mut Context<'_>,
        ) -> Poll<Option<Result<Frame<Bytes>, io::Error>>> {
            if !self.sent {
                self.sent = true;
                return Poll::Ready(Some(Ok(Frame::data(Bytes::from_static(b"first")))));
            }
            match self.pause.as_mut().poll(cx) {
                Poll::Ready(()) => Poll::Ready(Some(Err(io::Error::other("the daemon stopped")))),
                Poll::Pending => Poll::Pending,
            }
        }
    }

    #[test]
    fn call_gives_up_on_a_daemon_that_does_not_answer() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            serve_with(&path, || async { std::future::pending().await });
            let started = std::time::Instant::now();
            let err = call_within(
                path.to_str().unwrap(),
                "unison.S",
                "Get",
                b"",
                Duration::from_millis(300),
            )
            .await
            .err()
            .expect("a silent daemon answered");
            assert_eq!(err, "the daemon did not answer in time");
            assert!(started.elapsed() < Duration::from_secs(2));
        });
    }

    #[test]
    fn call_gives_up_on_a_response_that_does_not_end() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            serve_with(&path, || async {
                hyper::Response::new(Endless.map_err(|never| match never {}).boxed())
            });
            let err = call_within(
                path.to_str().unwrap(),
                "unison.S",
                "Get",
                b"",
                Duration::from_millis(300),
            )
            .await
            .err()
            .expect("an endless response was accepted");
            assert_eq!(err, "the daemon did not finish the response in time");
        });
    }

    #[test]
    fn call_reports_a_response_that_breaks_off() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            serve_with(&path, || async {
                hyper::Response::new(Broken::new().boxed())
            });
            let err = call(path.to_str().unwrap(), "unison.S", "Get", b"")
                .await
                .err()
                .expect("a broken response was accepted");
            assert!(err.starts_with("reading the response failed"), "{err}");
        });
    }

    #[test]
    fn call_reports_an_endpoint_that_does_not_speak_http2() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let listener = bind_socket(&path);
        block_on(async {
            listener.set_nonblocking(true).expect("nonblocking");
            let listener = tokio::net::UnixListener::from_std(listener).expect("listener");
            tokio::spawn(async move {
                // The peer reads nothing and closes the connection at once.
                let (stream, _) = listener.accept().await.expect("accept");
                drop(stream);
            });
            let err = tokio::time::timeout(
                Duration::from_secs(5),
                call(path.to_str().unwrap(), "unison.S", "Get", b""),
            )
            .await
            .expect("the call hung on a closed connection")
            .err()
            .expect("a closed connection answered");
            assert!(
                err.starts_with("cannot open a session with the daemon")
                    || err.starts_with("the request to the daemon failed"),
                "{err}"
            );
        });
    }

    #[test]
    fn call_refuses_an_invalid_procedure_before_connecting() {
        let err = block_on(call("/nonexistent/unisond.sock", "unison.S", "Get/x", b""))
            .err()
            .expect("an invalid procedure was called");
        assert_eq!(err, "the procedure name is not valid");
    }

    #[test]
    fn stream_reports_a_stream_that_breaks_off() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            serve_with(&path, || async {
                hyper::Response::new(Broken::new().boxed())
            });
            let mut chunks = Vec::new();
            let err = stream(path.to_str().unwrap(), "unison.S", "Watch", b"", |data| {
                chunks.extend_from_slice(data);
                Ok(())
            })
            .await
            .expect_err("a broken stream ended cleanly");
            assert!(err.starts_with("reading the event stream failed"), "{err}");
            assert_eq!(chunks, b"first", "the chunk before the break was lost");
        });
    }

    #[test]
    fn stream_waits_for_the_first_event_without_a_limit() {
        // The daemon answers a stream with the first event, which may come
        // later than the limit of a unary call.
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        block_on(async {
            serve_with(&path, || async {
                tokio::time::sleep(Duration::from_millis(500)).await;
                hyper::Response::new(
                    Full::new(Bytes::from_static(b"event"))
                        .map_err(|never| match never {})
                        .boxed(),
                )
            });
            let mut chunks = Vec::new();
            stream(path.to_str().unwrap(), "unison.S", "Watch", b"", |data| {
                chunks.extend_from_slice(data);
                Ok(())
            })
            .await
            .expect("stream");
            assert_eq!(chunks, b"event");
        });
    }

    #[test]
    fn api_response_serializes_for_the_webview() {
        let response = ApiResponse {
            status: 200,
            body: BASE64.encode(b"\x00\xff"),
        };
        assert_eq!(
            serde_json::to_string(&response).unwrap(),
            r#"{"status":200,"body":"AP8="}"#
        );
    }

    #[test]
    fn connect_accepts_a_private_socket() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        block_on(async {
            connect(path.to_str().unwrap()).await.expect("connect");
        });
    }

    #[test]
    fn connect_refuses_a_socket_other_users_can_reach() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        set_mode(&path, 0o666);
        let err = block_on(connect(path.to_str().unwrap()))
            .err()
            .expect("connected");
        assert_eq!(err.kind(), io::ErrorKind::PermissionDenied);
    }

    #[test]
    fn connect_refuses_a_directory_other_users_can_write() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        let _listener = bind_socket(&path);
        set_mode(dir.path(), 0o777);
        let err = block_on(connect(path.to_str().unwrap()))
            .err()
            .expect("connected");
        assert_eq!(err.kind(), io::ErrorKind::PermissionDenied);
    }

    #[test]
    fn connect_refuses_a_file() {
        let dir = TempDir::new();
        let path = dir.join("unisond.sock");
        std::fs::write(&path, b"").unwrap();
        set_mode(&path, 0o600);
        let err = block_on(connect(path.to_str().unwrap()))
            .err()
            .expect("connected");
        assert_eq!(err.kind(), io::ErrorKind::InvalidInput);
    }

    #[test]
    fn connect_refuses_a_symlink_to_the_socket() {
        let dir = TempDir::new();
        let target = dir.join("real.sock");
        let _listener = bind_socket(&target);
        let path = dir.join("unisond.sock");
        std::os::unix::fs::symlink(&target, &path).unwrap();
        let err = block_on(connect(path.to_str().unwrap()))
            .err()
            .expect("connected");
        assert_eq!(err.kind(), io::ErrorKind::InvalidInput);
    }
}
