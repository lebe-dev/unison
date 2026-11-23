//! Cancellation of the streaming calls the webviews open.
//!
//! A streaming call holds a connection to the daemon until the daemon ends the
//! response, and the daemon serves a bounded number of connections. A webview
//! that stops listening, or that reloads, therefore has to end the call on this
//! side too, otherwise every abandoned stream keeps a connection open.

use std::collections::HashMap;
use std::future::Future;
use std::pin::{Pin, pin};
use std::sync::{Mutex, MutexGuard};
use std::task::Poll;

use tokio::sync::oneshot;

/// Key names a stream by the label of the webview that opened it and the id
/// the webview chose for it.
type Key = (String, u32);

/// Entry is the cancel handle of one open stream. The serial tells a stream
/// apart from a later one that reuses its key after a reload.
struct Entry {
    serial: u64,
    cancel: oneshot::Sender<()>,
}

/// Streams keeps the cancel handles of the open streams.
#[derive(Default)]
pub struct Streams {
    state: Mutex<State>,
}

#[derive(Default)]
struct State {
    next: u64,
    open: HashMap<Key, Entry>,
}

impl Streams {
    /// open registers a stream. A stream that holds the same key already is
    /// cancelled, because its webview no longer knows about it.
    pub fn open(&self, owner: &str, id: u32) -> Stream<'_> {
        let (cancel, cancelled) = oneshot::channel();
        let key = (owner.to_string(), id);
        let mut state = self.lock();
        state.next += 1;
        let serial = state.next;
        // Dropping the previous sender ends the previous stream.
        state.open.insert(key.clone(), Entry { serial, cancel });
        Stream {
            streams: self,
            key,
            serial,
            cancelled,
        }
    }

    /// cancel ends one stream of a webview. It reports whether the stream was
    /// open.
    pub fn cancel(&self, owner: &str, id: u32) -> bool {
        let entry = self.lock().open.remove(&(owner.to_string(), id));
        match entry {
            Some(entry) => {
                let _ = entry.cancel.send(());
                true
            }
            None => false,
        }
    }

    /// cancel_owner ends every stream of a webview and returns how many were
    /// open.
    pub fn cancel_owner(&self, owner: &str) -> usize {
        let mut state = self.lock();
        let keys: Vec<Key> = state
            .open
            .keys()
            .filter(|(label, _)| label == owner)
            .cloned()
            .collect();
        for key in &keys {
            if let Some(entry) = state.open.remove(key) {
                let _ = entry.cancel.send(());
            }
        }
        keys.len()
    }

    /// len returns the number of open streams.
    #[cfg(test)]
    fn len(&self) -> usize {
        self.lock().open.len()
    }

    /// lock keeps the handles after a panic of another thread, so a failure
    /// never leaves a stream that cannot be cancelled.
    fn lock(&self) -> MutexGuard<'_, State> {
        self.state.lock().unwrap_or_else(|err| err.into_inner())
    }
}

/// Stream is a registered stream. It leaves the registry when it is dropped.
pub struct Stream<'a> {
    streams: &'a Streams,
    key: Key,
    serial: u64,
    cancelled: oneshot::Receiver<()>,
}

impl Stream<'_> {
    /// run drives work until it finishes or the stream is cancelled. A
    /// cancelled stream drops work, which closes its connection, and ends
    /// without an error because the webview asked for it.
    pub async fn run<F>(mut self, work: F) -> Result<(), String>
    where
        F: Future<Output = Result<(), String>>,
    {
        let mut work = pin!(work);
        let cancelled = &mut self.cancelled;
        std::future::poll_fn(|cx| {
            if let Poll::Ready(result) = work.as_mut().poll(cx) {
                return Poll::Ready(result);
            }
            // A dropped sender also ends the stream: the key was taken over.
            if Pin::new(&mut *cancelled).poll(cx).is_ready() {
                return Poll::Ready(Ok(()));
            }
            Poll::Pending
        })
        .await
    }
}

impl Drop for Stream<'_> {
    fn drop(&mut self) {
        let mut state = self.streams.lock();
        if state
            .open
            .get(&self.key)
            .is_some_and(|entry| entry.serial == self.serial)
        {
            state.open.remove(&self.key);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::future::pending;

    fn block_on<F: Future>(future: F) -> F::Output {
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .expect("build the runtime")
            .block_on(future)
    }

    #[test]
    fn finished_work_leaves_the_registry() {
        let streams = Streams::default();
        let result = block_on(
            streams
                .open("main", 1)
                .run(async { Err("closed".to_string()) }),
        );
        assert_eq!(result, Err("closed".to_string()));
        assert_eq!(streams.len(), 0);
    }

    #[test]
    fn cancel_ends_pending_work() {
        let streams = Streams::default();
        let stream = streams.open("main", 1);
        assert!(streams.cancel("main", 1));
        let result = block_on(stream.run(pending()));
        assert_eq!(result, Ok(()));
        assert_eq!(streams.len(), 0);
        assert!(!streams.cancel("main", 1), "a stream is cancelled twice");
    }

    #[test]
    fn cancel_owner_ends_only_the_streams_of_that_webview() {
        let streams = Streams::default();
        let first = streams.open("main", 1);
        let second = streams.open("main", 2);
        let other = streams.open("prompt", 1);
        assert_eq!(streams.cancel_owner("main"), 2);
        assert_eq!(block_on(first.run(pending())), Ok(()));
        assert_eq!(block_on(second.run(pending())), Ok(()));
        assert_eq!(streams.len(), 1);
        drop(other);
        assert_eq!(streams.len(), 0);
    }

    #[test]
    fn a_reused_key_ends_the_older_stream_and_keeps_the_newer_one() {
        let streams = Streams::default();
        let older = streams.open("main", 1);
        let newer = streams.open("main", 1);
        assert_eq!(block_on(older.run(pending())), Ok(()));
        // The older stream left without taking the entry of the newer one.
        assert_eq!(streams.len(), 1);
        assert!(streams.cancel("main", 1));
        assert_eq!(block_on(newer.run(pending())), Ok(()));
    }
}
