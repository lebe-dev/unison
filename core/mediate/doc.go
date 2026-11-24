// Package mediate introduces two devices through a third trusted device.
//
// The mediator knows both endpoints, the endpoints never discover each other on
// the network first. The exchange runs over the two sessions with the mediator,
// so it works on the local network and over the relay alike. Both endpoints
// confirm the introduction, and the mediator forwards the messages without
// learning the rendezvous key the endpoints derive.
package mediate
