package poker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// 快照是私密持久化数据，禁止交给HTTP。通过原始牌堆和完整行动日志
// 重建所有内部字段，解码不信任外部提供的派生余额、私牌或行动权。
type snapshot struct {
	Version int              `json:"version"`
	Config  HandConfig       `json:"config"`
	Deck    Deck             `json:"deck"`
	Actions []recordedAction `json:"actions"`
}

func EncodeState(s State) ([]byte, error) {
	if !s.valid {
		return nil, errors.New("INVALID_STATE")
	}
	return json.Marshal(snapshot{Version: 1, Config: s.cfg, Deck: s.deck, Actions: append([]recordedAction{}, s.history...)})
}

func DecodeState(data []byte) (State, error) {
	// 先检查重复键，避免JSON的“最后一个值生效”绕过版本或数值校验。
	keys := json.NewDecoder(bytes.NewReader(data))
	if checkSnapshotValue(keys) != nil {
		return State{}, errors.New("INVALID_SNAPSHOT")
	}
	var snap struct {
		Version int              `json:"version"`
		Config  *HandConfig      `json:"config"`
		Deck    []Card           `json:"deck"`
		Actions []recordedAction `json:"actions"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&snap) != nil {
		return State{}, errors.New("INVALID_SNAPSHOT")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return State{}, errors.New("INVALID_SNAPSHOT")
	}
	if snap.Version != 1 {
		return State{}, errors.New("SNAPSHOT_VERSION")
	}
	if snap.Config == nil || len(snap.Deck) != 52 || snap.Actions == nil {
		return State{}, errors.New("INVALID_SNAPSHOT")
	}
	var deck Deck
	copy(deck[:], snap.Deck)
	s, err := NewHand(*snap.Config, deck)
	if err != nil {
		return State{}, errors.New("INVALID_SNAPSHOT")
	}
	for _, a := range snap.Actions {
		s, err = Apply(s, a.Seat, a.Action)
		if err != nil {
			return State{}, errors.New("INVALID_SNAPSHOT")
		}
	}
	return s, nil
}

// 所有快照字段均非nullable；递归检查对象键（包括大小写别名）唯一。
func checkSnapshotValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("INVALID_SNAPSHOT")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("INVALID_SNAPSHOT")
			}
			name = strings.ToLower(name)
			if seen[name] {
				return errors.New("INVALID_SNAPSHOT")
			}
			seen[name] = true
			if err := checkSnapshotValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := checkSnapshotValue(d); err != nil {
				return err
			}
		}
	default:
		return errors.New("INVALID_SNAPSHOT")
	}
	_, err = d.Token()
	return err
}
