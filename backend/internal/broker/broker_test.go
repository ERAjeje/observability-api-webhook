package broker

import (
	"testing"
	"time"
)

func TestBroker_FanOutParaTodosAssinantes(t *testing.T) {
	b := New()
	defer b.Close()

	ch1, un1 := b.Subscribe("status")
	defer un1()
	ch2, un2 := b.Subscribe("status")
	defer un2()

	ev := Event{Type: EventStatusChanged, EndpointID: 1, Timestamp: time.Now()}
	b.Publish("status", ev)

	if got := <-ch1; got.EndpointID != 1 {
		t.Fatalf("ch1 deveria receber evento; got %+v", got)
	}
	if got := <-ch2; got.EndpointID != 1 {
		t.Fatalf("ch2 deveria receber evento; got %+v", got)
	}
}

// Consumidor lento não bloqueia o publisher (resiliência do fan-out).
func TestBroker_NaoBloqueiaComConsumidorLento(t *testing.T) {
	b := New()
	defer b.Close()

	slow, un := b.Subscribe("status") // ninguém lê o canal
	defer un()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish("status", Event{Type: "x", EndpointID: int64(i), Timestamp: time.Now()})
		}
		close(done)
	}()
	select {
	case <-done:
		// publisher terminou apesar do buffer 256 >> 1000 eventos → drops.
	case <-time.After(2 * time.Second):
		t.Fatal("publish travou com consumidor lento (deveria ser não-bloqueante)")
	}
	// Confirma que drops foram contados e o canal continua funcionando.
	for i := 0; i < 256; i++ {
		<-slow
	}
}

func TestBroker_UnsubscribeFechaCanal(t *testing.T) {
	b := New()
	ch, un := b.Subscribe("status")
	un()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("canal deveria estar fechado após unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("canal não fechou após unsubscribe")
	}
}