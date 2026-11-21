package tikhub

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// CreatorFollowerCount reads the follower total out of a creator_profile
// response. The paths are taken from real responses archived under
// tikhub/calls: douyin.creator_profile (handler_user_profile) answers
// data.user.follower_count, and xhs.creator_profile (get_user_info) answers
// data.data.fans.
//
// ok=false means the response did not carry a follower number this build can
// read. Callers must treat that as "not collected" and never store a zero as a
// verified value: an absent field and a real zero are different facts.
func CreatorFollowerCount(response map[string]interface{}, platform string) (int64, bool) {
	switch platform {
	case "douyin":
		return followerAtPath(response, "data", "user", "follower_count")
	case "xhs":
		return followerAtPath(response, "data", "data", "fans")
	default:
		return 0, false
	}
}

func followerAtPath(response map[string]interface{}, path ...string) (int64, bool) {
	var current interface{} = response
	for _, key := range path {
		container, ok := current.(map[string]interface{})
		if !ok {
			return 0, false
		}
		current, ok = container[key]
		if !ok {
			return 0, false
		}
	}
	return followerNumber(current)
}

// followerNumber accepts the shapes a real profile response carries: a JSON
// number (the client decodes with UseNumber, so json.Number is the normal case)
// or a plain digit string. Unit-suffixed strings such as "1.2万" are rejected on
// purpose — no verified sample defines the unit for these two endpoints, and
// guessing a multiplier would turn an unknown value into a wrong one.
func followerNumber(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil || parsed < 0 {
			return 0, false
		}
		return parsed, true
	case float64:
		if typed < 0 || math.Trunc(typed) != typed || typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return typed, true
	case int:
		if typed < 0 {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil || parsed < 0 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
