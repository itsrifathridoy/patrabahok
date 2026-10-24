package ops

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// QueuedMessage is one message in the Postfix queue, from `postqueue -j`.
type QueuedMessage struct {
	ID         string
	Queue      string // active, deferred, hold, incoming
	Arrival    time.Time
	Size       int64
	Sender     string
	Recipients []QueuedRecipient
}

type QueuedRecipient struct {
	Address string
	Reason  string
}

// FirstReason is the first recipient's delay reason, for list views.
func (m QueuedMessage) FirstReason() string {
	for _, r := range m.Recipients {
		if r.Reason != "" {
			return r.Reason
		}
	}
	return ""
}

var queueIDRe = regexp.MustCompile(`^[0-9A-Za-z]{5,32}$`)

// Queue lists queued messages, oldest first.
func Queue() ([]QueuedMessage, error) {
	out, err := exec.Command("postqueue", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("postqueue: %w", err)
	}
	var msgs []QueuedMessage
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var raw struct {
			QueueName   string `json:"queue_name"`
			QueueID     string `json:"queue_id"`
			ArrivalTime int64  `json:"arrival_time"`
			MessageSize int64  `json:"message_size"`
			Sender      string `json:"sender"`
			Recipients  []struct {
				Address     string `json:"address"`
				DelayReason string `json:"delay_reason"`
			} `json:"recipients"`
		}
		if json.Unmarshal(sc.Bytes(), &raw) != nil {
			continue
		}
		m := QueuedMessage{ID: raw.QueueID, Queue: raw.QueueName, Arrival: time.Unix(raw.ArrivalTime, 0), Size: raw.MessageSize, Sender: raw.Sender}
		if m.Sender == "" {
			m.Sender = "<> (bounce)"
		}
		for _, r := range raw.Recipients {
			m.Recipients = append(m.Recipients, QueuedRecipient{Address: r.Address, Reason: r.DelayReason})
		}
		msgs = append(msgs, m)
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Arrival.Before(msgs[j].Arrival) })
	return msgs, nil
}

func postsuper(args ...string) error {
	out, err := exec.Command("postsuper", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("postsuper: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteMessage removes one message from the queue permanently.
func DeleteMessage(id string) error {
	if !queueIDRe.MatchString(id) {
		return errors.New("invalid queue ID")
	}
	return postsuper("-d", id)
}

// RetryMessage asks Postfix to attempt delivery of one message now.
func RetryMessage(id string) error {
	if !queueIDRe.MatchString(id) {
		return errors.New("invalid queue ID")
	}
	out, err := exec.Command("postqueue", "-i", id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("postqueue: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// HoldMessage / ReleaseMessage park a message without deleting it, and put it back.
func HoldMessage(id string) error {
	if !queueIDRe.MatchString(id) {
		return errors.New("invalid queue ID")
	}
	return postsuper("-h", id)
}

func ReleaseMessage(id string) error {
	if !queueIDRe.MatchString(id) {
		return errors.New("invalid queue ID")
	}
	return postsuper("-H", id)
}

// DeleteAllDeferred removes every message Postfix has given up on for now.
func DeleteAllDeferred() error { return postsuper("-d", "ALL", "deferred") }

// FlushQueue retries delivery of everything queued.
func FlushQueue() error {
	out, err := exec.Command("postqueue", "-f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("postqueue: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
