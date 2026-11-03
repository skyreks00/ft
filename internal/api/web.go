package api

import (
	"net/url"
	"strconv"
)

// IntraProjectsURL hosts the project pages of the intra web UI, used when
// an action is not available through the API.
const IntraProjectsURL = "https://projects.intra.42.fr"

// ProjectPageURL is a project's page on the intra.
func ProjectPageURL(slug string) string {
	return IntraProjectsURL + "/projects/" + url.PathEscape(slug)
}

// SlotsPageURL is the intra page where a team books evaluation slots.
func SlotsPageURL(slug string, teamID int) string {
	return ProjectPageURL(slug) + "/slots?team_id=" + strconv.Itoa(teamID)
}

// slotsPlatforms are campuses that manage evaluation slots and bookings on
// their own platform; slots created on the intra are deleted there.
var slotsPlatforms = map[int]string{
	12: "https://slots.42belgium.be", // 42 Belgium (formerly slots.s19.be)
}

// SlotsPlatform returns the external slots platform for the user's campus,
// or "" when slots are managed on the intra. override (FT_SLOTS_URL) wins;
// "intra" forces the intra.
func SlotsPlatform(override string, me *User) string {
	switch override {
	case "intra":
		return ""
	case "":
	default:
		return override
	}
	if me == nil {
		return ""
	}
	if c := me.PrimaryCampus(); c != nil {
		return slotsPlatforms[c.ID]
	}
	return ""
}
