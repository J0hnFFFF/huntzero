package eventbus

import "zdll/internal/event"

// Bus is a typed publish/subscribe channel.
type Bus interface {
	Publish(e event.Event)
	Subscribe() <-chan event.Event
	Unsubscribe(ch <-chan event.Event)
}
