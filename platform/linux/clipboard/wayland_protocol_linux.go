//go:build linux

package clipboard

import (
	"sync"

	"github.com/SpinningVinyl/go-wayland/wayland/client"
	"golang.org/x/sys/unix"
)

// Interface names of the data-control protocols. Both protocols have the same
// request and event layout, only the names differ.
const (
	// dataControlExt is the name of the staging protocol of wayland-protocols.
	dataControlExt = "ext_data_control_manager_v1"
	// dataControlWLR is the name of the wlroots protocol, which wlroots
	// compositors offered before the staging one existed.
	dataControlWLR = "zwlr_data_control_manager_v1"
)

// dcManager is the manager object of a data-control protocol.
type dcManager struct {
	client.BaseProxy
}

// newDCManager registers a manager object that the registry bind fills in.
func newDCManager(ctx *client.Context) *dcManager {
	m := &dcManager{}
	ctx.Register(m)
	return m
}

// createDataSource creates the source that describes content to offer.
func (m *dcManager) createDataSource() (*dcSource, error) {
	source := &dcSource{}
	m.Context().Register(source)
	if err := request(m, 0, u32Arg(source.ID())); err != nil {
		m.Context().Unregister(source)
		return nil, err
	}
	return source, nil
}

// getDataDevice returns the device that watches and sets the selection of a
// seat.
func (m *dcManager) getDataDevice(seat *client.Seat) (*dcDevice, error) {
	device := &dcDevice{}
	m.Context().Register(device)
	body := append(u32Arg(device.ID()), u32Arg(seat.ID())...)
	if err := request(m, 1, body); err != nil {
		m.Context().Unregister(device)
		return nil, err
	}
	return device, nil
}

// dcDevice is the device of one seat. It reports the selection of the seat and
// takes it over.
type dcDevice struct {
	client.BaseProxy

	offerHandler     func(*dcOffer)
	selectionHandler func(*dcOffer)
	finishedHandler  func()
}

// setOfferHandler receives the offer of every announced selection.
func (d *dcDevice) setOfferHandler(f func(*dcOffer)) { d.offerHandler = f }

// setSelectionHandler receives the selection, nil when it was cleared.
func (d *dcDevice) setSelectionHandler(f func(*dcOffer)) { d.selectionHandler = f }

// setFinishedHandler receives the loss of the device.
func (d *dcDevice) setFinishedHandler(f func()) { d.finishedHandler = f }

// setSelection takes the selection. A nil source clears it.
func (d *dcDevice) setSelection(source *dcSource) error {
	var id uint32
	if source != nil {
		id = source.ID()
	}
	return request(d, 0, u32Arg(id))
}

// Dispatch handles the events of the device. An offer of the primary selection
// is destroyed again: the clipboard synchronizes CLIPBOARD only.
func (d *dcDevice) Dispatch(opcode uint32, fd int, body []byte) {
	closeReceivedFD(fd)
	switch opcode {
	case 0:
		if len(body) < 4 {
			return
		}
		offer := &dcOffer{}
		d.Context().RegisterServer(offer, client.Uint32(body[0:4]))
		if d.offerHandler != nil {
			d.offerHandler(offer)
		}
	case 1:
		if len(body) < 4 {
			return
		}
		d.selection(selectionOffer(d.Context(), client.Uint32(body[0:4])))
	case 2:
		if d.finishedHandler != nil {
			d.finishedHandler()
		}
	case 3:
		if len(body) < 4 {
			return
		}
		offer := selectionOffer(d.Context(), client.Uint32(body[0:4]))
		if offer == nil {
			return
		}
		_ = offer.destroy()
	}
}

// selection tells the handler about the selection of the event.
func (d *dcDevice) selection(offer *dcOffer) {
	if d.selectionHandler == nil {
		return
	}
	d.selectionHandler(offer)
}

// selectionOffer returns the registered offer of an event, nil for a cleared
// selection and for an identifier the compositor never announced.
func selectionOffer(ctx *client.Context, id uint32) *dcOffer {
	if id == 0 {
		return nil
	}
	offer, ok := ctx.GetProxy(id).(*dcOffer)
	if !ok {
		return nil
	}
	return offer
}

// dcSource is the source side of a transfer. It carries the content the
// clipboard serves.
type dcSource struct {
	client.BaseProxy

	sendHandler      func(mime string, fd int)
	cancelledHandler func()
}

// setSendHandler receives a request of a client for a value.
func (s *dcSource) setSendHandler(f func(string, int)) { s.sendHandler = f }

// setCancelledHandler receives the loss of the selection.
func (s *dcSource) setCancelledHandler(f func()) { s.cancelledHandler = f }

// offer adds a MIME type to the source.
func (s *dcSource) offer(mime string) error {
	return request(s, 0, stringArg(mime))
}

// destroy releases the source object.
func (s *dcSource) destroy() error {
	err := request(s, 1, nil)
	s.Context().Unregister(s)
	return err
}

// Dispatch handles the events of the source.
func (s *dcSource) Dispatch(opcode uint32, fd int, body []byte) {
	switch opcode {
	case 0:
		if s.sendHandler == nil {
			closeReceivedFD(fd)
			return
		}
		s.sendHandler(stringAt(body, 0), fd)
	case 1:
		closeReceivedFD(fd)
		if s.cancelledHandler != nil {
			s.cancelledHandler()
		}
	}
}

// TakesFD reports that a send event carries the pipe of the transfer.
func (s *dcSource) TakesFD(opcode uint32) bool { return opcode == 0 }

// dcOffer is one offer of another client. Its MIME types arrive before the
// selection event that carries the offer.
type dcOffer struct {
	client.BaseProxy

	mu    sync.Mutex
	mimes targetList
}

// receive asks the owner for a value of the MIME type and passes the write end
// of the transfer pipe.
func (o *dcOffer) receive(mime string, fd int) error {
	return requestWithFD(o, 0, stringArg(mime), fd)
}

// destroy releases the offer object.
func (o *dcOffer) destroy() error {
	err := request(o, 1, nil)
	o.Context().Unregister(o)
	return err
}

// targets returns the MIME types the owner advertised.
func (o *dcOffer) targets() targetList {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append(targetList(nil), o.mimes...)
}

// Dispatch handles the events of the offer.
func (o *dcOffer) Dispatch(opcode uint32, fd int, body []byte) {
	closeReceivedFD(fd)
	if opcode != 0 {
		return
	}
	mime := stringAt(body, 0)
	if mime == "" {
		return
	}
	o.mu.Lock()
	o.mimes = append(o.mimes, mime)
	o.mu.Unlock()
}

// request writes one request of a protocol object.
func request(proxy client.Proxy, opcode uint32, body []byte) error {
	return proxy.Context().WriteMsg(requestMessage(proxy, opcode, body), nil)
}

// requestWithFD writes one request that carries a file descriptor.
func requestWithFD(proxy client.Proxy, opcode uint32, body []byte, fd int) error {
	return proxy.Context().WriteMsg(requestMessage(proxy, opcode, body), unix.UnixRights(fd))
}

// requestMessage builds the wire message of a request.
func requestMessage(proxy client.Proxy, opcode uint32, body []byte) []byte {
	size := 8 + len(body)
	buf := make([]byte, size)
	client.PutUint32(buf[0:4], proxy.ID())
	client.PutUint32(buf[4:8], uint32(size)<<16|opcode&0x0000ffff)
	copy(buf[8:], body)
	return buf
}

// u32Arg encodes an object identifier or another 32 bit argument.
func u32Arg(value uint32) []byte {
	buf := make([]byte, 4)
	client.PutUint32(buf, value)
	return buf
}

// stringArg encodes a string argument with its length and padding.
func stringArg(value string) []byte {
	padded := client.PaddedLen(len(value) + 1)
	buf := make([]byte, 4+padded)
	client.PutString(buf, value, padded)
	return buf
}

// stringAt decodes a string argument of an event body. A truncated body yields
// an empty string instead of a panic, because the body comes from the
// compositor.
func stringAt(body []byte, offset int) string {
	if len(body) < offset+4 {
		return ""
	}
	length := int(client.Uint32(body[offset:]))
	padded := client.PaddedLen(length)
	if length < 1 || len(body) < offset+4+padded {
		return ""
	}
	return client.String(body[offset+4 : offset+4+padded])
}

// closeReceivedFD closes a descriptor that arrived with an event the clipboard
// does not use.
func closeReceivedFD(fd int) {
	if fd >= 0 {
		_ = unix.Close(fd)
	}
}
