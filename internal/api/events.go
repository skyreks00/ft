package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const eventsTTL = 10 * time.Minute

// Event is a campus event.
type Event struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Kind           string    `json:"kind"`
	Location       string    `json:"location"`
	BeginAt        time.Time `json:"begin_at"`
	EndAt          time.Time `json:"end_at"`
	MaxPeople      *int      `json:"max_people"`
	NbrSubscribers int       `json:"nbr_subscribers"`
	CursusIDs      []int     `json:"cursus_ids"`
	// Set (to a delay, in the intra's units) when leaving is restricted.
	ProhibitionOfCancellation any `json:"prohibition_of_cancellation"`
}

// Limited reports whether the event has a seat limit (0 means none).
func (e *Event) Limited() bool { return e.MaxPeople != nil && *e.MaxPeople > 0 }

// Full reports whether the event has no seat left.
func (e *Event) Full() bool {
	return e.Limited() && e.NbrSubscribers >= *e.MaxPeople
}

// EventsUser is a user's subscription to an event.
type EventsUser struct {
	ID      int   `json:"id"`
	EventID int   `json:"event_id"`
	UserID  int   `json:"user_id"`
	Event   Event `json:"event"`
}

// ErrNotSubscribed is returned when leaving an event one has not joined.
var ErrNotSubscribed = errors.New("you are not subscribed to this event")

func futureSorted() url.Values {
	return url.Values{"filter[future]": {"true"}, "sort": {"begin_at"}}
}

// CampusEvents lists upcoming events of a campus for a cursus, soonest
// first.
func (c *Client) CampusEvents(ctx context.Context, campusID, cursusID int) ([]Event, error) {
	path := "/v2/campus/" + strconv.Itoa(campusID) + "/cursus/" + strconv.Itoa(cursusID) + "/events"
	return GetAll[Event](ctx, c, path, futureSorted(), eventsTTL)
}

// MyEvents lists the upcoming events a user subscribed to.
func (c *Client) MyEvents(ctx context.Context, userID int) ([]Event, error) {
	return GetAll[Event](ctx, c, "/v2/users/"+strconv.Itoa(userID)+"/events", futureSorted(), time.Minute)
}

// Event fetches one event.
func (c *Client) Event(ctx context.Context, id int) (*Event, error) {
	var e Event
	if err := c.Get(ctx, "/v2/events/"+strconv.Itoa(id), nil, eventsTTL, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// JoinEvent subscribes a user to an event.
func (c *Client) JoinEvent(ctx context.Context, eventID, userID int) error {
	body := map[string]any{"events_user": map[string]int{"event_id": eventID, "user_id": userID}}
	err := c.Send(ctx, http.MethodPost, "/v2/events_users", body, nil)
	c.ForgetList("/v2/users/"+strconv.Itoa(userID)+"/events", futureSorted())
	return err
}

// LeaveEvent unsubscribes a user from an event.
func (c *Client) LeaveEvent(ctx context.Context, eventID, userID int) error {
	q := url.Values{"filter[event_id]": {strconv.Itoa(eventID)}}
	var subs []EventsUser
	if err := c.Get(ctx, "/v2/users/"+strconv.Itoa(userID)+"/events_users", q, 0, &subs); err != nil {
		return err
	}
	if len(subs) == 0 {
		return ErrNotSubscribed
	}
	err := c.Send(ctx, http.MethodDelete, "/v2/events_users/"+strconv.Itoa(subs[0].ID), nil, nil)
	c.ForgetList("/v2/users/"+strconv.Itoa(userID)+"/events", futureSorted())
	return err
}
