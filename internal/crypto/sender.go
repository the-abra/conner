package crypto

import (
	"sync"
)

// SenderState is a per-sender symmetric ratchet for group rooms (sender keys).
// Each member encrypts with their own chain; others store a copy of that chain.
type SenderState struct {
	mu      sync.Mutex
	chains  map[string]*Ratchet // sender nick -> ratchet
	myNick  string
	myChain *Ratchet
	epoch   uint64
}

func NewSenderState(nick string, epoch uint64, seed []byte) *SenderState {
	s := &SenderState{
		chains: make(map[string]*Ratchet),
		myNick: nick,
		epoch:  epoch,
	}
	if len(seed) > 0 {
		s.myChain = NewRatchet(seed)
		s.chains[nick] = s.myChain
	}
	return s
}

func (s *SenderState) Epoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch
}

func (s *SenderState) ResetEpoch(epoch uint64, mySeed []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch = epoch
	s.chains = make(map[string]*Ratchet)
	if len(mySeed) > 0 {
		s.myChain = NewRatchet(mySeed)
		s.chains[s.myNick] = s.myChain
	}
}

func (s *SenderState) AdoptPeer(nick string, seed []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chains[nick] = NewRatchet(seed)
}

func (s *SenderState) ExportMySeed() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.myChain == nil {
		return nil
	}
	out := make([]byte, len(s.myChain.GetInitialKey()))
	copy(out, s.myChain.GetInitialKey())
	return out
}

func (s *SenderState) NextSend() (key []byte, step uint32, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.myChain == nil {
		return nil, 0, false
	}
	k, st := s.myChain.Next()
	return k, st, true
}

func (s *SenderState) HasPeer(nick string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.chains[nick]
	return ok
}

func (s *SenderState) Recv(nick string, step uint32) ([]byte, error) {
	s.mu.Lock()
	r, ok := s.chains[nick]
	s.mu.Unlock()
	if !ok {
		return nil, errNoChain
	}
	return r.GetMessageKey(step)
}

var errNoChain = errString("no sender chain for peer")

type errString string

func (e errString) Error() string { return string(e) }
