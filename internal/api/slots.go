package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SlotRange is a run of contiguous slots with the same booked state. The
// intra stores slots as 15-minute chunks; ranges are what users think in.
type SlotRange struct {
	IDs     []int     `json:"ids"`
	BeginAt time.Time `json:"begin_at"`
	EndAt   time.Time `json:"end_at"`
	Booked  bool      `json:"booked"`
}

// ID identifies the range by its first chunk.
func (r SlotRange) ID() int { return r.IDs[0] }

// GroupSlots merges contiguous chunks; slots must be sorted by begin time.
func GroupSlots(slots []Slot) []SlotRange {
	var out []SlotRange
	for _, s := range slots {
		booked := s.ScaleTeam != nil
		if n := len(out); n > 0 && out[n-1].EndAt.Equal(s.BeginAt) && out[n-1].Booked == booked {
			out[n-1].IDs = append(out[n-1].IDs, s.ID)
			out[n-1].EndAt = s.EndAt
			continue
		}
		out = append(out, SlotRange{IDs: []int{s.ID}, BeginAt: s.BeginAt, EndAt: s.EndAt, Booked: booked})
	}
	return out
}

func mySlotsQuery() url.Values {
	return url.Values{"filter[future]": {"true"}, "sort": {"begin_at"}}
}

// MySlots lists the authenticated user's upcoming evaluation slots.
func (c *Client) MySlots(ctx context.Context) ([]Slot, error) {
	return GetAll[Slot](ctx, c, "/v2/me/slots", mySlotsQuery(), time.Minute)
}

// CreateSlot opens evaluation slots between begin and end. The intra
// answers with one slot or with its 15-minute chunks.
func (c *Client) CreateSlot(ctx context.Context, userID int, begin, end time.Time) ([]Slot, error) {
	body := map[string]any{"slot": map[string]any{
		"user_id":  userID,
		"begin_at": begin.UTC().Format(time.RFC3339),
		"end_at":   end.UTC().Format(time.RFC3339),
	}}
	var raw json.RawMessage
	err := c.Send(ctx, http.MethodPost, "/v2/slots", body, &raw)
	c.ForgetList("/v2/me/slots", mySlotsQuery())
	if err != nil {
		return nil, err
	}
	var many []Slot
	if json.Unmarshal(raw, &many) == nil {
		return many, nil
	}
	var one Slot
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, err
	}
	return []Slot{one}, nil
}

// DeleteSlot removes one slot chunk.
func (c *Client) DeleteSlot(ctx context.Context, id int) error {
	err := c.Send(ctx, http.MethodDelete, "/v2/slots/"+strconv.Itoa(id), nil, nil)
	c.ForgetList("/v2/me/slots", mySlotsQuery())
	return err
}
