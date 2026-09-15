package crowdstrike

import "context"

// eventService bridges the event source to an eventSink: validate the sink once,
// then route each event the source delivers. The sink is a port (client.go), so
// Stage 2's CrowdStrike push and Stage 1's logging share this file unchanged,
// differing only in which sink is wired in.
type eventService struct {
	source *eventSource
	sink   eventSink
	rep    *reporter
}

func newEventService(source *eventSource, sink eventSink, rep *reporter) *eventService {
	return &eventService{source: source, sink: sink, rep: rep}
}

// run validates the sink once, then blocks in the source until ctx is cancelled.
func (s *eventService) run(ctx context.Context) error {
	if err := s.sink.validate(ctx); err != nil {
		return err
	}

	return s.source.subscribe(ctx, func(ev *packageGuardEvent) error {
		return s.handleEvent(ctx, ev)
	})
}

// handleEvent routes one event to the sink best-effort: a send error is logged,
// never fatal, so one bad event does not stop the stream or strand the cursor.
func (s *eventService) handleEvent(ctx context.Context, ev *packageGuardEvent) error {
	if err := s.sink.send(ctx, ev); err != nil {
		s.rep.logWarn("Send failed for event %s: %v", ev.GetEventId(), err)
	}
	return nil
}
