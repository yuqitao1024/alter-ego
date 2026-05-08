package lark

import (
	"testing"
	"time"
)

func TestMessageDeduperRejectsDuplicateWithinTTL(t *testing.T) {
	now := time.Unix(100, 0)
	deduper := newMessageDeduper(5*time.Minute, func() time.Time { return now })

	if !deduper.MarkIfNew("om_1") {
		t.Fatal("first message should be accepted")
	}
	if deduper.MarkIfNew("om_1") {
		t.Fatal("duplicate message should be rejected within ttl")
	}
}

func TestMessageDeduperAllowsMessageAgainAfterTTL(t *testing.T) {
	now := time.Unix(100, 0)
	deduper := newMessageDeduper(5*time.Minute, func() time.Time { return now })

	if !deduper.MarkIfNew("om_1") {
		t.Fatal("first message should be accepted")
	}

	now = now.Add(6 * time.Minute)

	if !deduper.MarkIfNew("om_1") {
		t.Fatal("message should be accepted again after ttl")
	}
}

func TestMessageDeduperAllowsEmptyMessageID(t *testing.T) {
	deduper := newMessageDeduper(5*time.Minute, time.Now)

	if !deduper.MarkIfNew("") {
		t.Fatal("empty message id should not be deduplicated")
	}
	if !deduper.MarkIfNew("") {
		t.Fatal("empty message id should not be deduplicated on repeat")
	}
}
