//! A local HTTP server for the tests of the updater.

use std::sync::Arc;

use http_body_util::Full;
use hyper::body::Bytes;
use hyper::service::service_fn;
use hyper::{HeaderMap, Response, StatusCode};
use hyper_util::rt::TokioIo;
use tokio::net::TcpListener;

/// MockRequest is what a handler of the server reads about a request.
#[derive(Clone, Debug)]
pub struct MockRequest {
    pub path: String,
    pub headers: HeaderMap,
}

/// Reply is the answer of a handler.
#[derive(Clone, Debug)]
pub struct Reply {
    pub status: u16,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
}

impl Reply {
    /// ok returns a body with the status 200.
    pub fn ok(body: impl Into<Vec<u8>>) -> Self {
        Self {
            status: 200,
            headers: Vec::new(),
            body: body.into(),
        }
    }
}

/// MockServer serves the answers of a handler until it is dropped.
pub struct MockServer {
    base_url: String,
    handle: tokio::task::JoinHandle<()>,
}

impl MockServer {
    /// start binds the server on a free port of the loopback interface.
    pub async fn start<F>(handler: F) -> Self
    where
        F: Fn(&MockRequest) -> Reply + Send + Sync + 'static,
    {
        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind the test server");
        let address = listener.local_addr().expect("read the address");
        let handler = Arc::new(handler);
        let handle = tokio::spawn(async move {
            loop {
                let Ok((stream, _)) = listener.accept().await else {
                    break;
                };
                let handler = Arc::clone(&handler);
                tokio::spawn(async move {
                    let service =
                        service_fn(move |request: hyper::Request<hyper::body::Incoming>| {
                            let handler = Arc::clone(&handler);
                            async move {
                                let view = MockRequest {
                                    path: request
                                        .uri()
                                        .path_and_query()
                                        .map(|path| path.to_string())
                                        .unwrap_or_default(),
                                    headers: request.headers().clone(),
                                };
                                let reply = handler(&view);
                                let mut response = Response::builder().status(
                                    StatusCode::from_u16(reply.status).expect("a valid status"),
                                );
                                for (name, value) in &reply.headers {
                                    response = response.header(name.as_str(), value.as_str());
                                }
                                #[allow(clippy::let_and_return)]
                                let response = response
                                    .body(Full::new(Bytes::from(reply.body)))
                                    .expect("build the answer");
                                Ok::<_, std::convert::Infallible>(response)
                            }
                        });
                    let _ = hyper::server::conn::http1::Builder::new()
                        .serve_connection(TokioIo::new(stream), service)
                        .await;
                });
            }
        });
        Self {
            base_url: format!("http://{address}"),
            handle,
        }
    }

    /// base returns the address of the server without a trailing slash.
    pub fn base(&self) -> &str {
        &self.base_url
    }

    /// url returns the address of a path.
    pub fn url(&self, path: &str) -> String {
        format!("{}{path}", self.base_url)
    }
}

impl Drop for MockServer {
    fn drop(&mut self) {
        self.handle.abort();
    }
}
