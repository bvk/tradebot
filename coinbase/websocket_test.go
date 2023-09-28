// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "testing"

func TestWebsocket(t *testing.T) {
	if !checkCredentials() {
		t.Skip("no credentials")
		return
	}

	c, err := New(testingKey, testingSecret, testingOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	ws, err := c.NewWebsocket(c.ctx, []string{"BCH-USD"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseWebsocket(ws)

	if err := ws.Subscribe(c.ctx, "level2"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ws.Unsubscribe(c.ctx, "level2"); err != nil {
			t.Fatal(err)
		}
	}()

	for {
		msg, err := ws.NextMessage(c.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Channel != "l2_data" {
			continue
		}
		t.Logf("%#v", msg)
		break
	}
}
