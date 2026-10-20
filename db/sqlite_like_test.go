package db

import (
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
)

func TestLikeCacheSemanticsAndBound(t *testing.T) {
	for _, tc := range []struct {
		args []driver.Value
		want driver.Value
		fail bool
	}{
		{[]driver.Value{"%世_", "你好世界"}, true, false},
		{[]driver.Value{"É%", "éclair"}, true, false},
		{[]driver.Value{"a!_%", "a_world", "!"}, true, false},
		{[]driver.Value{"a!_%", "abworld", "!"}, false, false},
		{[]driver.Value{"a!", "a!", "!"}, false, false},
		{[]driver.Value{"a_%", "abc"}, true, false},
		{[]driver.Value{"a_%", "abc", "_"}, false, false},
		{[]driver.Value{"a_%", "a%", "_"}, true, false},
		{[]driver.Value{"a%", "abc", nil}, nil, false},
		{[]driver.Value{nil, "abc"}, nil, false},
		{[]driver.Value{"%", nil}, nil, false},
		{[]driver.Value{"%", "abc", "ab"}, nil, true},
		{[]driver.Value{"a%", "a\nx"}, true, false},
	} {
		for i := 0; i < 2; i++ {
			got, err := sqliteLike(tc.args)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("%v: %v, %v", tc.args, got, err)
			}
		}
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				sqliteLike([]driver.Value{fmt.Sprintf("%d-%d%%", n, i), "value"})
			}
		}(n)
	}
	wg.Wait()
	likeCache.Lock()
	defer likeCache.Unlock()
	if len(likeCache.entries) > likeCacheLimit || likeCache.order.Len() != len(likeCache.entries) {
		t.Fatalf("unbounded/inconsistent cache: %d", len(likeCache.entries))
	}
}
