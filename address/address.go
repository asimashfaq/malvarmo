package address

import (
	"bytes"
	"fmt"
	"log"

	"golang.org/x/crypto/sha3"
)

// makeAddress returns the address based on the public spend key and the public view key
func makeAddress(pubSpend, pubView PublicKey) []byte {
	// A Monero address 'mAddr' looks as follows:
	// c = netBytePrefix(0x12) | publicSpendKey | publicViewKey
	// mAddr = base58encode(c | checksum(c)[:4])
	const netBytePrefix = byte(18)
	buf := make([]byte, 0, 69)
	buf = append(buf, netBytePrefix)
	buf = append(buf, pubSpend...)
	buf = append(buf, pubView...)
	h := sha3.NewLegacyKeccak256()
	h.Write(buf)
	hash := h.Sum(nil)
	buf = append(buf, hash[:4]...)
	return base58encode(buf)
}

func New() (*KeyPair, *KeyPair, []byte, error) {
	spendKeyPair, err := newSpendKeyPair()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create new spend key pair: %s", err.Error())
	}
	viewKeyPair := makeViewKeyPair(spendKeyPair.PrivateKey())
	address := makeAddress(spendKeyPair.PublicKey(), viewKeyPair.PublicKey())
	return spendKeyPair, viewKeyPair, address, nil
}

func NewWithPrefix(prefix []byte, numWorkers int) (*KeyPair, *KeyPair, []byte, error) {
	type result struct {
		spendKeyPair, viewKeyPair *KeyPair
		address                   []byte
	}

	spawn := func(wid int, ch chan<- *result, done <-chan struct{}) error {
		spendKeyPair, err := newSpendKeyPair()
		if err != nil {
			return fmt.Errorf("failed to create new spend key pair: %s", err.Error())
		}
		go func() {
			nextSpendKeyPair := nextSpendKeyPairMaker(spendKeyPair)
			var viewKeyPair *KeyPair
			address := make([]byte, 2)
			for !bytes.HasPrefix(address[2:], prefix) {
				select {
				case <-done:
					break
				default:
					nextSpendKeyPair()
					viewKeyPair = makeViewKeyPair(spendKeyPair.PrivateKey())
					address = makeAddress(spendKeyPair.PublicKey(), viewKeyPair.PublicKey())
				}
			}
			// Try to send our result
			// TODO(leon): There still is a data race here, i.e. send on closed channel.
			select {
			case <-done:
				// Closed
			default:
				ch <- &result{
					viewKeyPair:  viewKeyPair,
					spendKeyPair: spendKeyPair,
					address:      address,
				}
			}
		}()
		return nil
	}

	ch := make(chan *result)
	var rch chan<- *result = ch
	done := make(chan struct{})
	for i := 0; i < numWorkers; i++ {
		if err := spawn(i, rch, done); err != nil {
			log.Printf("%s, retrying", err.Error())
			i-- // Retry
		}
	}
	res := <-ch
	close(done)
	close(ch)

	return res.spendKeyPair, res.viewKeyPair, res.address, nil
}
