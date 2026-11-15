// Package policy maps peer role, network and item size to an action.
//
// Decide is a pure function over a configuration and an input. Decider binds
// the configuration to the live trust store, network state and connection path
// and implements clip.PolicyDecider.
package policy
