// Package nodetest runs several nodes in one process. It binds every socket on
// an in-memory network, supplies the platform ports, controls the network
// conditions and pairs the devices, so the tests of the core cover the
// end-to-end scenarios without real sockets.
//
// A Harness owns the virtual network, the discovery bus and the virtual clock,
// and releases them at the end of the test. Every node keeps its data in a
// temporary directory.
package nodetest
