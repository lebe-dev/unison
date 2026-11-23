// Package relay implements the rendezvous and datagram relay server. It moves
// opaque datagrams between the members of a mailbox and keeps no state on disk
// and no identities: clients authenticate with an access token and a mailbox is
// a pair of connections that share an id. Mailboxes are scoped to the token, so
// the groups served by one relay never meet.
package relay
