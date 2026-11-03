package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGroupSlots(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 3, h, m, 0, 0, time.UTC) }
	booked := &struct {
		ID int `json:"id"`
	}{ID: 9}
	slots := []Slot{
		{ID: 1, BeginAt: at(14, 0), EndAt: at(14, 15)},
		{ID: 2, BeginAt: at(14, 15), EndAt: at(14, 30)},
		{ID: 3, BeginAt: at(14, 30), EndAt: at(14, 45), ScaleTeam: booked},
		{ID: 4, BeginAt: at(14, 45), EndAt: at(15, 0)},
		{ID: 5, BeginAt: at(16, 0), EndAt: at(16, 15)},
	}
	got := GroupSlots(slots)
	if len(got) != 4 {
		t.Fatalf("got %d ranges: %+v", len(got), got)
	}
	if got[0].ID() != 1 || len(got[0].IDs) != 2 || !got[0].EndAt.Equal(at(14, 30)) || got[0].Booked {
		t.Errorf("range 0 = %+v", got[0])
	}
	if !got[1].Booked || got[2].ID() != 4 || got[3].ID() != 5 {
		t.Errorf("ranges = %+v", got)
	}
}

func TestCorrectedsForms(t *testing.T) {
	var s []ScaleTeam
	err := json.Unmarshal([]byte(`[{"correcteds":"invisible"},{"correcteds":[{"login":"a"},{"login":"b"}]}]`), &s)
	if err != nil {
		t.Fatal(err)
	}
	if len(s[0].Correcteds) != 0 || len(s[1].Correcteds) != 2 || s[1].Correcteds[1].Login != "b" {
		t.Fatalf("got %+v", s)
	}
}

func TestCreateSlotBodyAndForms(t *testing.T) {
	for name, reply := range map[string]string{
		"array":  `[{"id":1,"begin_at":"2026-11-03T13:00:00Z","end_at":"2026-11-03T13:15:00Z"},{"id":2}]`,
		"object": `{"id":1,"begin_at":"2026-11-03T13:00:00Z","end_at":"2026-11-03T15:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var body map[string]map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v2/slots" {
					http.NotFound(w, r)
					return
				}
				b, _ := io.ReadAll(r.Body)
				json.Unmarshal(b, &body)
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, reply)
			}))
			defer srv.Close()
			c, _ := newTestClient(srv, nil)
			loc := time.FixedZone("CET", 3600)
			begin := time.Date(2026, 11, 3, 14, 0, 0, 0, loc)
			slots, err := c.CreateSlot(context.Background(), 7, begin, begin.Add(2*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if len(slots) == 0 || slots[0].ID != 1 {
				t.Fatalf("slots = %+v", slots)
			}
			s := body["slot"]
			if s["user_id"] != float64(7) || s["begin_at"] != "2026-11-03T13:00:00Z" || s["end_at"] != "2026-11-03T15:00:00Z" {
				t.Fatalf("body = %v", body)
			}
		})
	}
}

func TestLeaveEvent(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/users/7/events_users":
			if r.URL.Query().Get("filter[event_id]") == "45" {
				fmt.Fprint(w, `[{"id":999,"event_id":45,"user_id":7}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case r.Method == http.MethodDelete:
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(srv, nil)
	if err := c.LeaveEvent(context.Background(), 45, 7); err != nil {
		t.Fatal(err)
	}
	if deleted != "/v2/events_users/999" {
		t.Fatalf("deleted %q", deleted)
	}
	if err := c.LeaveEvent(context.Background(), 46, 7); err != ErrNotSubscribed {
		t.Fatalf("err = %v, want ErrNotSubscribed", err)
	}
}

func TestEventSeats(t *testing.T) {
	zero, ten := 0, 10
	cases := []struct {
		max           *int
		subs          int
		limited, full bool
	}{{nil, 5, false, false}, {&zero, 5, false, false}, {&ten, 9, true, false}, {&ten, 10, true, true}}
	for _, c := range cases {
		e := Event{MaxPeople: c.max, NbrSubscribers: c.subs}
		if e.Limited() != c.limited || e.Full() != c.full {
			t.Errorf("%+v: limited=%v full=%v", c, e.Limited(), e.Full())
		}
	}
}

func TestSlotsPlatform(t *testing.T) {
	belgium := &User{Campus: []Campus{{ID: 12}}, CampusUsers: []CampusUser{{CampusID: 12, IsPrimary: true}}}
	paris := &User{Campus: []Campus{{ID: 1}}}
	cases := []struct {
		override string
		me       *User
		want     string
	}{
		{"", belgium, "https://slots.42belgium.be"},
		{"", paris, ""},
		{"intra", belgium, ""},
		{"https://slots.example", paris, "https://slots.example"},
		{"", nil, ""},
	}
	for _, c := range cases {
		if got := SlotsPlatform(c.override, c.me); got != c.want {
			t.Errorf("SlotsPlatform(%q) = %q, want %q", c.override, got, c.want)
		}
	}
}
