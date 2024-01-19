package logdir

import (
	"log"
	"testing"
)

func TestLogDir(t *testing.T) {
	b, err := New("/tmp", "testlogdir", 1)
	if err != nil {
		t.Fatal(err)
	}
	log := log.New(b, "", log.Flags())
	for i := 0; i < 1024*1024; i++ {
		log.Printf("hello world")
	}
}
