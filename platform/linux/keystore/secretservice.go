package keystore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lebe-dev/unison/core/ports"
)

// D-Bus names of the Secret Service.
const (
	busName  = "org.freedesktop.secrets"
	pathBus  = dbus.ObjectPath("/org/freedesktop/DBus")
	ifaceBus = "org.freedesktop.DBus"

	pathService     = dbus.ObjectPath("/org/freedesktop/secrets")
	ifaceService    = "org.freedesktop.Secret.Service"
	ifaceCollection = "org.freedesktop.Secret.Collection"
	ifaceItem       = "org.freedesktop.Secret.Item"
	ifacePrompt     = "org.freedesktop.Secret.Prompt"

	// emptyPath is what the service returns for an absent alias or prompt.
	emptyPath = dbus.ObjectPath("/")

	// Aliases of the collections secrets are stored in.
	aliasDefault = "default"
	aliasLogin   = "login"

	// Attributes of an item.
	attrApplication = "application"
	attrService     = "service"
	attrAccount     = "account"

	// applicationName is the value of the application attribute.
	applicationName = "app.unison"

	// Item properties.
	propLabel      = "org.freedesktop.Secret.Item.Label"
	propAttributes = "org.freedesktop.Secret.Item.Attributes"

	// Algorithms of OpenSession.
	algorithmDH    = "dh-ietf1024-sha256-aes128-cbc-pkcs7"
	algorithmPlain = "plain"

	// secretContentType is the type of the stored values.
	secretContentType = "text/plain"
)

// aesKeyLen is the key length of the session encryption.
const aesKeyLen = 16

// secretValue is the Secret struct of the protocol: (oayays).
type secretValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// Session reports the Secret Service of the desktop session.
type Session struct {
	// Bus reports that the session bus accepted a connection.
	Bus bool
	// Service reports that the Secret Service owns its bus name.
	Service bool
	// Alias is the collection secrets are stored in: default, login, or empty
	// when the keyring has neither.
	Alias string
	// Locked reports that the collection must be unlocked before use.
	Locked bool
}

// Inspect reports the Secret Service of the session. It opens no session and
// shows no prompt; a missing bus or service is reported in the result.
func Inspect() (Session, error) {
	conn, ok := connectSession()
	if !ok {
		return Session{}, nil
	}
	owned, err := serviceOnBus(conn)
	if err != nil {
		return Session{Bus: true}, fmt.Errorf("keystore: ask for the Secret Service: %w", err)
	}
	if !owned {
		return Session{Bus: true}, nil
	}
	session := Session{Bus: true, Service: true}
	service := conn.Object(busName, pathService)
	for _, alias := range []string{aliasDefault, aliasLogin} {
		var path dbus.ObjectPath
		if err := service.Call(ifaceService+".ReadAlias", 0, alias).Store(&path); err != nil {
			return session, fmt.Errorf("keystore: read the %s collection: %w", alias, err)
		}
		if path == emptyPath || path == "" {
			continue
		}
		locked, err := lockedAt(conn, path, ifaceCollection)
		if err != nil {
			// The alias names a collection the service does not export yet, so
			// it cannot be used; keep looking at the other alias.
			continue
		}
		session.Alias = alias
		session.Locked = locked
		break
	}
	return session, nil
}

// connectSession returns the session bus, or nil when the session has none or
// the platform has no Secret Service.
func connectSession() (busConn, bool) {
	conn, err := openSessionBus()
	if err != nil {
		return nil, false
	}
	return conn, true
}

// secretService keeps secrets in the Secret Service of the session.
type secretService struct {
	conn    busConn
	logger  *slog.Logger
	timeout time.Duration
	session dbus.ObjectPath
	key     []byte
}

// newSecretService opens a session with the service.
func newSecretService(conn busConn, timeout time.Duration, logger *slog.Logger) (*secretService, error) {
	s := &secretService{conn: conn, logger: logger, timeout: timeout}
	if err := s.openSession(); err != nil {
		return nil, err
	}
	// The session can hold a Secret Service without a collection the secrets
	// could go to, for example a locked login keyring of an autologin session.
	if _, err := s.collection(); err != nil {
		return nil, err
	}
	return s, nil
}

// openSession opens an encrypted session, and a plain one when the service does
// not support the key exchange.
func (s *secretService) openSession() error {
	service := s.conn.Object(busName, pathService)
	private, public, err := dhKeyPair()
	if err != nil {
		return err
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	err = service.Call(ifaceService+".OpenSession", 0, algorithmDH, dbus.MakeVariant(public)).Store(&output, &session)
	if err == nil {
		peer, ok := output.Value().([]byte)
		if !ok {
			return errors.New("keystore: the Secret Service returned an unexpected public key")
		}
		s.session = session
		s.key = dhSharedKey(private, peer)
		return nil
	}
	// A service without the key exchange gets a plain session. The secrets then
	// cross the bus unencrypted, which is what the warning in the log is about.
	s.logger.Warn("the Secret Service does not support an encrypted session, the secrets cross the bus unencrypted", "error", err)
	err = service.Call(ifaceService+".OpenSession", 0, algorithmPlain, dbus.MakeVariant("")).Store(&output, &session)
	if err != nil {
		return fmt.Errorf("keystore: open a session with the Secret Service: %w", err)
	}
	s.session = session
	return nil
}

// load returns the secret stored under name.
func (s *secretService) load(ctx context.Context, service, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := s.search(ctx, service, name)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("keystore: load %q: %w", name, ports.ErrNotFound)
	}
	var value secretValue
	if err := s.conn.Object(busName, items[0]).Call(ifaceItem+".GetSecret", 0, s.session).Store(&value); err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	secret, err := s.unseal(value.Value)
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	return secret, nil
}

// searchAnswer reads the answer of SearchItems: the unlocked items and the
// locked ones. The specification names two lists, but GNOME Keyring answers
// with the unlocked items only.
func searchAnswer(body []any) (unlocked, locked []dbus.ObjectPath, err error) {
	if len(body) == 0 || len(body) > 2 {
		return nil, nil, fmt.Errorf("the service returned %d lists, want one or two", len(body))
	}
	lists := make([][]dbus.ObjectPath, len(body))
	for i, value := range body {
		items, ok := value.([]dbus.ObjectPath)
		if !ok {
			return nil, nil, fmt.Errorf("the service returned %T, want object paths", value)
		}
		lists[i] = items
	}
	unlocked = lists[0]
	if len(lists) == 2 {
		locked = lists[1]
	}
	return unlocked, locked, nil
}

// store replaces the secret stored under name.
func (s *secretService) store(ctx context.Context, service, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	collection, err := s.collection()
	if err != nil {
		return err
	}
	if err := s.ensureUnlocked(ctx, collection, ifaceCollection); err != nil {
		return err
	}
	value, err := s.seal(secret)
	if err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	properties := map[string]dbus.Variant{
		propLabel:      dbus.MakeVariant("Unison " + name),
		propAttributes: dbus.MakeVariant(attributes(service, name)),
	}
	body := secretValue{Session: s.session, Parameters: []byte{}, Value: value, ContentType: secretContentType}
	var path, prompt dbus.ObjectPath
	call := s.conn.Object(busName, collection).Call(ifaceCollection+".CreateItem", 0, properties, body, true)
	if err := call.Store(&path, &prompt); err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	if err := s.answerPrompt(ctx, prompt); err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	return nil
}

// remove deletes every item with the attributes of the secret.
func (s *secretService) remove(ctx context.Context, service, name string) error {
	items, err := s.search(ctx, service, name)
	if err != nil {
		return err
	}
	for _, item := range items {
		var prompt dbus.ObjectPath
		if err := s.conn.Object(busName, item).Call(ifaceItem+".Delete", 0).Store(&prompt); err != nil {
			return fmt.Errorf("keystore: delete %q: %w", name, err)
		}
		if err := s.answerPrompt(ctx, prompt); err != nil {
			return fmt.Errorf("keystore: delete %q: %w", name, err)
		}
	}
	return nil
}

// search returns the items of the secret, unlocked.
func (s *secretService) search(ctx context.Context, service, name string) ([]dbus.ObjectPath, error) {
	collection, err := s.collection()
	if err != nil {
		return nil, err
	}
	if err := s.ensureUnlocked(ctx, collection, ifaceCollection); err != nil {
		return nil, err
	}
	call := s.conn.Object(busName, collection).Call(ifaceCollection+".SearchItems", 0, attributes(service, name))
	if call.Err != nil {
		return nil, fmt.Errorf("keystore: search the keyring: %w", call.Err)
	}
	unlocked, locked, err := searchAnswer(call.Body)
	if err != nil {
		return nil, fmt.Errorf("keystore: search the keyring: %w", err)
	}
	for _, item := range locked {
		if err := s.ensureUnlocked(ctx, item, ifaceItem); err != nil {
			return nil, err
		}
	}
	return append(unlocked, locked...), nil
}

// collection returns the collection secrets are stored in: the default alias,
// the login alias when there is no default. It never creates a collection.
func (s *secretService) collection() (dbus.ObjectPath, error) {
	service := s.conn.Object(busName, pathService)
	var cause error
	for _, alias := range []string{aliasDefault, aliasLogin} {
		var path dbus.ObjectPath
		if err := service.Call(ifaceService+".ReadAlias", 0, alias).Store(&path); err != nil {
			return "", fmt.Errorf("keystore: read the %s collection: %w", alias, err)
		}
		if path == emptyPath || path == "" {
			continue
		}
		// An alias can name a collection that the service does not export, as
		// a login keyring does while it is locked.
		if _, err := lockedAt(s.conn, path, ifaceCollection); err != nil {
			cause = err
			continue
		}
		return path, nil
	}
	if cause != nil {
		return "", fmt.Errorf("%w: the keyring has no usable collection: %w", ErrKeyringUnavailable, cause)
	}
	return "", fmt.Errorf("%w: the keyring has neither a %s nor a %s collection", ErrKeyringUnavailable, aliasDefault, aliasLogin)
}

// ready reports whether the collection of the secrets can be used: it exists
// and it is unlocked, with a system prompt when it is locked.
func (s *secretService) ready(ctx context.Context) error {
	collection, err := s.collection()
	if err != nil {
		return err
	}
	return s.ensureUnlocked(ctx, collection, ifaceCollection)
}

// ensureUnlocked unlocks path and waits for the system prompt.
func (s *secretService) ensureUnlocked(ctx context.Context, path dbus.ObjectPath, iface string) error {
	locked, err := lockedAt(s.conn, path, iface)
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	call := s.conn.Object(busName, pathService).Call(ifaceService+".Unlock", 0, []dbus.ObjectPath{path})
	if err := call.Store(&unlocked, &prompt); err != nil {
		return fmt.Errorf("keystore: unlock the keyring: %w", err)
	}
	if err := s.answerPrompt(ctx, prompt); err != nil {
		return err
	}
	locked, err = lockedAt(s.conn, path, iface)
	if err != nil {
		return err
	}
	if locked {
		return ErrKeyringLocked
	}
	return nil
}

// lockedAt reports whether the collection or the item at path is locked.
func lockedAt(conn busConn, path dbus.ObjectPath, iface string) (bool, error) {
	property, err := conn.Object(busName, path).GetProperty(iface + ".Locked")
	if err != nil {
		return false, fmt.Errorf("keystore: read the lock state: %w", err)
	}
	locked, ok := property.Value().(bool)
	if !ok {
		return false, errors.New("keystore: the service returned a lock state that is not a boolean")
	}
	return locked, nil
}

// answerPrompt shows a system prompt and waits until the user answers it.
func (s *secretService) answerPrompt(ctx context.Context, prompt dbus.ObjectPath) error {
	if prompt == "" || prompt == emptyPath {
		return nil
	}
	accepted, err := s.prompt(ctx, prompt)
	if err != nil {
		return err
	}
	if !accepted {
		return fmt.Errorf("%w: the prompt was dismissed", ErrKeyringLocked)
	}
	return nil
}

// prompt shows a system prompt and waits for its completion.
func (s *secretService) prompt(ctx context.Context, path dbus.ObjectPath) (bool, error) {
	signals := make(chan *dbus.Signal, 8)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	// The call returns only when the user answers the prompt, so it runs on its
	// own goroutine and the timer below bounds the wait.
	answers := make(chan error, 1)
	go func() {
		answers <- s.conn.Object(busName, path).Call(ifacePrompt+".Prompt", 0, "Unison needs the keyring").Err
	}()
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.dismiss(path)
			return false, ctx.Err()
		case <-timer.C:
			s.dismiss(path)
			return false, fmt.Errorf("%w: no answer within %s", ErrKeyringLocked, s.timeout)
		case err := <-answers:
			if err != nil {
				return false, fmt.Errorf("keystore: show the keyring prompt: %w", err)
			}
		case signal, ok := <-signals:
			if !ok {
				return false, errors.New("keystore: the bus closed the signal channel")
			}
			if signal.Path != path || signal.Name != ifacePrompt+".Completed" {
				continue
			}
			if len(signal.Body) == 0 {
				return false, errors.New("keystore: the prompt reported no result")
			}
			accepted, ok := signal.Body[0].(bool)
			if !ok {
				return false, errors.New("keystore: the prompt reported a result that is not a boolean")
			}
			return accepted, nil
		}
	}
}

// dismiss closes a prompt that nobody answered, so the dialog does not stay on
// the screen after the wait is over.
func (s *secretService) dismiss(path dbus.ObjectPath) {
	go func() {
		_ = s.conn.Object(busName, path).Call(ifacePrompt+".Dismiss", 0).Err
	}()
}

// seal encrypts the secret for the session.
func (s *secretService) seal(secret []byte) ([]byte, error) {
	if s.key == nil {
		return secret, nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(secret, block.BlockSize())
	out := make([]byte, len(padded))
	//nolint:gosec // The Secret Service session uses the CBC mode with a zero IV by protocol.
	cipher.NewCBCEncrypter(block, make([]byte, block.BlockSize())).CryptBlocks(out, padded)
	return out, nil
}

// unseal decrypts a secret of the session.
func (s *secretService) unseal(value []byte) ([]byte, error) {
	if s.key == nil {
		return value, nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 || len(value)%block.BlockSize() != 0 {
		return nil, errors.New("keystore: the secret item holds no valid ciphertext")
	}
	out := make([]byte, len(value))
	cipher.NewCBCDecrypter(block, make([]byte, block.BlockSize())).CryptBlocks(out, value)
	return pkcs7Unpad(out, block.BlockSize())
}

// attributes returns the attributes of a secret.
func attributes(service, name string) map[string]string {
	return map[string]string{
		attrApplication: applicationName,
		attrService:     service,
		attrAccount:     name,
	}
}

// serviceOnBus reports whether the Secret Service owns its bus name.
func serviceOnBus(conn busConn) (bool, error) {
	var owned bool
	call := conn.Object(ifaceBus, pathBus).Call(ifaceBus+".NameHasOwner", 0, busName)
	if err := call.Store(&owned); err != nil {
		return false, err
	}
	return owned, nil
}

// dhPrime is the 1024 bit MODP group of the key exchange.
var dhPrime, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF", 16)

// dhGenerator is the generator of the key exchange.
var dhGenerator = big.NewInt(2)

// dhKeyPair returns a private key and the matching public key of the exchange.
func dhKeyPair() (*big.Int, []byte, error) {
	buf := make([]byte, 128)
	if _, err := rand.Read(buf); err != nil {
		return nil, nil, fmt.Errorf("keystore: generate a session key: %w", err)
	}
	private := new(big.Int).SetBytes(buf)
	public := new(big.Int).Exp(dhGenerator, private, dhPrime)
	return private, public.Bytes(), nil
}

// dhSharedKey returns the AES key of the exchange with the public key of the
// service.
func dhSharedKey(private *big.Int, peer []byte) []byte {
	shared := new(big.Int).Exp(new(big.Int).SetBytes(peer), private, dhPrime)
	// The service hashes the shared secret padded to the length of the prime.
	padded := make([]byte, dhPrime.BitLen()/8)
	shared.FillBytes(padded)
	sum := sha256.Sum256(padded)
	return sum[:aesKeyLen]
}

// pkcs7Pad pads data to the block size.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

// pkcs7Unpad removes the padding of data.
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("keystore: the secret item has a wrong length")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("keystore: the secret item has an invalid padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, errors.New("keystore: the secret item has an invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}
