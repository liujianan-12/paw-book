package paychannel

import (
	"net/url"
	"testing"
	"time"
)

func TestParseOptionsDefaults(t *testing.T) {
	opts := ParseOptions(url.Values{})
	if opts.Behavior != BehaviorSuccess {
		t.Errorf("默认行为应为 success，得到 %s", opts.Behavior)
	}
	if opts.Repeat != 1 {
		t.Errorf("默认重复次数应为 1，得到 %d", opts.Repeat)
	}
	if opts.Delay != 0 {
		t.Errorf("默认延迟应为 0，得到 %v", opts.Delay)
	}
}

func TestParseOptionsFull(t *testing.T) {
	opts := ParseOptions(url.Values{
		"behavior": []string{"timeout"},
		"repeat":   []string{"3"},
		"delay":    []string{"2s"},
	})
	if opts.Behavior != BehaviorTimeout {
		t.Errorf("行为应为 timeout，得到 %s", opts.Behavior)
	}
	if opts.Repeat != 3 {
		t.Errorf("重复次数应为 3，得到 %d", opts.Repeat)
	}
	if opts.Delay != 2*time.Second {
		t.Errorf("延迟应为 2s，得到 %v", opts.Delay)
	}
}

func TestParseOptionsInvalidValues(t *testing.T) {
	opts := ParseOptions(url.Values{
		"behavior": []string{"hack"},
		"repeat":   []string{"-5"},
		"delay":    []string{"abc"},
	})
	if opts.Behavior != BehaviorSuccess {
		t.Errorf("非法行为应回退 success，得到 %s", opts.Behavior)
	}
	if opts.Repeat != 1 {
		t.Errorf("非法重复次数应回退 1，得到 %d", opts.Repeat)
	}
	if opts.Delay != 0 {
		t.Errorf("非法延迟应回退 0，得到 %v", opts.Delay)
	}
}
