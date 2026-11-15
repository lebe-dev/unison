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
use http_body_util::{BodyExt, Full};
use hyper::body::{Bytes, Incoming};
use hyper::client::conn::http2;
use hyper::header::CONTENT_TYPE;
use hyper::{Method, Request, StatusCode};
use hyper_util::rt::{TokioExecutor, TokioIo};
use serde::Serialize;
use tokio::io::{AsyncRead, AsyncWrite, ReadBuf};

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

/// Connect opens the endpoint of the daemon.
pub async fn connect(endpoint: &str) -> io::Result<EndpointStream> {
    #[cfg(unix)]
    {
        tokio::net::UnixStream::connect(endpoint)
            .await
            .map(EndpointStream::Unix)
    }
    #[cfg(windows)]
    {
        tokio::net::windows::named_pipe::ClientOptions::new()
            .open(endpoint)
            .map(EndpointStream::Pipe)
    }
}

/// Call performs a unary call of the service.
pub async fn call(
    endpoint: &str,
    service: &str,
    method: &str,
    body: &[u8],
) -> Result<ApiResponse, String> {
    let request = Bytes::copy_from_slice(body);
    let (response, _sender) = open(
        endpoint,
        service,
        method,
        UNARY_CONTENT_TYPE,
        request,
        Some(REQUEST_TIMEOUT),
    )
    .await?;
    let status = response.status();
    let body = tokio::time::timeout(
        REQUEST_TIMEOUT,
        read_body(response.into_body(), MAX_RESPONSE_BYTES),
    )
    .await
    .map_err(|_| "the daemon did not finish the response in time".to_string())??;
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
        let frame = frame.map_err(|err| format!("reading the event stream failed: {err}"))?;
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
    let stream = connect(endpoint)
        .await
        .map_err(|err| format!("cannot reach the daemon: {err}"))?;
    let (mut sender, connection) = http2::handshake(TokioExecutor::new(), TokioIo::new(stream))
        .await
        .map_err(|err| format!("cannot open a session with the daemon: {err}"))?;
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
        Some(limit) => tokio::time::timeout(limit, pending)
            .await
            .map_err(|_| "the daemon did not answer in time".to_string())?,
        None => pending.await,
    }
    .map_err(|err| format!("the request to the daemon failed: {err}"))?;
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

/// read_body collects a response body up to limit bytes.
async fn read_body(body: Incoming, limit: usize) -> Result<Vec<u8>, String> {
    let collected = body
        .collect()
        .await
        .map_err(|err| format!("reading the response failed: {err}"))?
        .to_bytes();
    if collected.len() > limit {
        return Err(format!("the response is larger than {limit} bytes"));
    }
    Ok(collected.to_vec())
}
