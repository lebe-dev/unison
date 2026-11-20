package keystore

import (
	"errors"
	"sync"

	"github.com/godbus/dbus/v5"
)

// fakeKeyring is a busConn that answers the calls of the Secret Service
// backend. It keeps items in memory and encrypts them with the key of the
// session it opened.
type fakeKeyring struct {
	mu    sync.Mutex
	chans []chan<- *dbus.Signal

	// service reports whether the Secret Service owns its bus name.
	service bool
	// rejectDH makes OpenSession fail for the encrypted algorithm.
	rejectDH bool
	// defaultAlias and loginAlias are the collections of the aliases. An empty
	// path means the alias does not exist.
	defaultAlias dbus.ObjectPath
	loginAlias   dbus.ObjectPath
	// absentCollections names the collections the service does not export, as
	// a locked login keyring of an autologin session.
	absentCollections map[dbus.ObjectPath]bool

	// collectionLocked and itemLocked are the lock states the properties report.
	collectionLocked bool
	itemLocked       bool
	// dismissPrompt answers every prompt with a refusal.
	dismissPrompt bool
	// silentPrompt leaves every prompt unanswered.
	silentPrompt bool
	// promptWait blocks the call that shows a prompt until the channel is
	// closed, which is what a real prompt does while it waits for the user.
	promptWait chan struct{}
	// singleSearch answers SearchItems with the unlocked list only, as GNOME
	// Keyring does.
	singleSearch bool

	items map[dbus.ObjectPath]*fakeItem

	// key is the shared key of the last session, nil for a plain one.
	key      []byte
	plain    bool
	sessions int

	created        int
	lastProps      map[string]dbus.Variant
	lastValue      []byte
	lastRaw        []byte
	lastCollection dbus.ObjectPath
}

// fakeItem is a secret item of the fake keyring.
type fakeItem struct {
	label  string
	attrs  map[string]string
	secret []byte
}

// implemented paths of the fake keyring.
const (
	fakeSession = dbus.ObjectPath("/org/freedesktop/secrets/session/s1")
	fakePrompt  = dbus.ObjectPath("/org/freedesktop/secrets/prompt/p1")
	fakeItemOne = dbus.ObjectPath("/org/freedesktop/secrets/collection/login/1")
	fakeItemTwo = dbus.ObjectPath("/org/freedesktop/secrets/collection/login/2")
)

// newFakeKeyring returns a keyring with a default collection and no items.
func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{
		service:      true,
		defaultAlias: "/org/freedesktop/secrets/collection/default",
		loginAlias:   "/org/freedesktop/secrets/collection/login",
		items:        make(map[dbus.ObjectPath]*fakeItem),
	}
}

// Object returns the fake object at path.
func (k *fakeKeyring) Object(_ string, path dbus.ObjectPath) busObject {
	return &fakeObject{ring: k, path: path}
}

// Signal registers a channel that receives the emitted signals.
func (k *fakeKeyring) Signal(ch chan<- *dbus.Signal) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.chans = append(k.chans, ch)
}

// RemoveSignal unregisters a channel.
func (k *fakeKeyring) RemoveSignal(ch chan<- *dbus.Signal) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i, registered := range k.chans {
		if registered == ch {
			k.chans = append(k.chans[:i], k.chans[i+1:]...)
			return
		}
	}
}

// emitLocked sends a signal to every registered channel while the lock is held.
func (k *fakeKeyring) emitLocked(path dbus.ObjectPath, name string, body []any) {
	chans := append([]chan<- *dbus.Signal(nil), k.chans...)
	signal := &dbus.Signal{Path: path, Name: name, Body: body}
	for _, ch := range chans {
		ch <- signal
	}
}

// fakeObject is a remote object of the fake keyring.
type fakeObject struct {
	ring *fakeKeyring
	path dbus.ObjectPath
}

// Call runs a method of the fake keyring.
func (o *fakeObject) Call(method string, _ dbus.Flags, args ...any) *dbus.Call {
	call := &dbus.Call{
		Path:   o.path,
		Method: method,
		Args:   args,
		Done:   make(chan *dbus.Call, 1),
	}
	call.Body, call.Err = o.ring.call(o.path, method, args)
	call.Done <- call
	close(call.Done)
	return call
}

// GetProperty returns a property of the fake object.
func (o *fakeObject) GetProperty(name string) (dbus.Variant, error) {
	return o.ring.property(o.path, name)
}

// call answers a method call.
func (k *fakeKeyring) call(path dbus.ObjectPath, method string, args []any) ([]any, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	switch method {
	case ifaceBus + ".NameHasOwner":
		return []any{k.service}, nil
	case ifaceService + ".OpenSession":
		return k.openSession(args)
	case ifaceService + ".ReadAlias":
		alias, _ := args[0].(string)
		switch alias {
		case aliasDefault:
			return []any{orEmpty(k.defaultAlias)}, nil
		case aliasLogin:
			return []any{orEmpty(k.loginAlias)}, nil
		}
		return []any{emptyPath}, nil
	case ifaceService + ".Unlock":
		return []any{[]dbus.ObjectPath{}, fakePrompt}, nil
	case ifacePrompt + ".Prompt":
		return k.answerPrompt()
	case ifaceCollection + ".SearchItems":
		return k.searchItems(args)
	case ifaceCollection + ".CreateItem":
		return k.createItem(path, args)
	case ifaceItem + ".GetSecret":
		return k.getSecret(path)
	case ifaceItem + ".Delete":
		delete(k.items, path)
		return []any{emptyPath}, nil
	}
	return nil, dbus.MakeFailedError(errors.New("fake keyring: unknown method " + method))
}

// openSession answers OpenSession.
func (k *fakeKeyring) openSession(args []any) ([]any, error) {
	algorithm, _ := args[0].(string)
	input, _ := args[1].(dbus.Variant)
	if algorithm == algorithmDH {
		if k.rejectDH {
			return nil, dbus.MakeFailedError(errors.New("fake keyring: the algorithm is not supported"))
		}
		client, ok := input.Value().([]byte)
		if !ok {
			return nil, dbus.MakeFailedError(errors.New("fake keyring: the client key is not a byte array"))
		}
		private, public, err := dhKeyPair()
		if err != nil {
			return nil, err
		}
		k.key = dhSharedKey(private, client)
		k.sessions++
		return []any{dbus.MakeVariant(public), fakeSession}, nil
	}
	if algorithm != algorithmPlain {
		return nil, dbus.MakeFailedError(errors.New("fake keyring: unknown algorithm"))
	}
	k.plain = true
	k.key = nil
	k.sessions++
	return []any{dbus.MakeVariant(""), fakeSession}, nil
}

// answerPrompt completes a prompt the way the test asked for.
func (k *fakeKeyring) answerPrompt() ([]any, error) {
	if k.promptWait != nil {
		wait := k.promptWait
		k.mu.Unlock()
		<-wait
		k.mu.Lock()
	}
	if k.silentPrompt {
		return nil, nil
	}
	accepted := !k.dismissPrompt
	if accepted {
		k.collectionLocked = false
		k.itemLocked = false
	}
	k.emitLocked(fakePrompt, ifacePrompt+".Completed", []any{accepted, dbus.MakeVariant([]dbus.ObjectPath{})})
	return []any{}, nil
}

// searchItems answers SearchItems.
func (k *fakeKeyring) searchItems(args []any) ([]any, error) {
	attrs, _ := args[0].(map[string]string)
	var unlocked, locked []dbus.ObjectPath
	for path, item := range k.items {
		if !matchAttrs(item.attrs, attrs) {
			continue
		}
		if k.collectionLocked || k.itemLocked {
			locked = append(locked, path)
			continue
		}
		unlocked = append(unlocked, path)
	}
	if k.singleSearch {
		return []any{unlocked}, nil
	}
	return []any{unlocked, locked}, nil
}

// createItem answers CreateItem.
func (k *fakeKeyring) createItem(path dbus.ObjectPath, args []any) ([]any, error) {
	props, _ := args[0].(map[string]dbus.Variant)
	value, ok := args[1].(secretValue)
	if !ok {
		return nil, dbus.MakeFailedError(errors.New("fake keyring: the item is not a secret"))
	}
	secret, err := (&secretService{key: k.key}).unseal(value.Value)
	if err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	item := &fakeItem{
		label:  stringProp(props, propLabel),
		attrs:  attrsProp(props),
		secret: secret,
	}
	itemPath := fakeItemOne
	if len(k.items) > 0 {
		itemPath = fakeItemTwo
	}
	k.items[itemPath] = item
	k.created++
	k.lastProps = props
	k.lastValue = secret
	k.lastRaw = append([]byte(nil), value.Value...)
	k.lastCollection = path
	return []any{itemPath, emptyPath}, nil
}

// getSecret answers GetSecret.
func (k *fakeKeyring) getSecret(path dbus.ObjectPath) ([]any, error) {
	item, ok := k.items[path]
	if !ok {
		return nil, dbus.MakeFailedError(errors.New("fake keyring: no such item"))
	}
	value, err := (&secretService{key: k.key}).seal(item.secret)
	if err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	return []any{secretValue{Session: fakeSession, Parameters: []byte{}, Value: value, ContentType: secretContentType}}, nil
}

// property answers a property read.
func (k *fakeKeyring) property(path dbus.ObjectPath, name string) (dbus.Variant, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	switch name {
	case ifaceCollection + ".Locked":
		if k.absentCollections[path] {
			return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", []any{"Object does not exist at path " + string(path)})
		}
		return dbus.MakeVariant(k.collectionLocked), nil
	case ifaceItem + ".Locked":
		if _, ok := k.items[path]; !ok {
			return dbus.Variant{}, errors.New("fake keyring: no such item")
		}
		return dbus.MakeVariant(k.itemLocked), nil
	}
	return dbus.Variant{}, errors.New("fake keyring: unknown property " + name)
}

// orEmpty returns the path, or the empty path when the alias is missing.
func orEmpty(path dbus.ObjectPath) dbus.ObjectPath {
	if path == "" {
		return emptyPath
	}
	return path
}

// matchAttrs reports whether the attributes of an item hold every wanted
// attribute.
func matchAttrs(have, want map[string]string) bool {
	for key, value := range want {
		if have[key] != value {
			return false
		}
	}
	return true
}

// stringProp returns a string property of the item.
func stringProp(props map[string]dbus.Variant, name string) string {
	value, ok := props[name]
	if !ok {
		return ""
	}
	text, _ := value.Value().(string)
	return text
}

// attrsProp returns the attribute property of the item.
func attrsProp(props map[string]dbus.Variant) map[string]string {
	value, ok := props[propAttributes]
	if !ok {
		return nil
	}
	attrs, _ := value.Value().(map[string]string)
	return attrs
}
