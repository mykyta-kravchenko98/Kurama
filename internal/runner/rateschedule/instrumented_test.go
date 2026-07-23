package rateschedule

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstrumentedObservesResultsAndPreservesResponse(t *testing.T) {
	t.Parallel()
	scheduleErr := errors.New("redis unavailable")
	tests := []struct {
		name       string
		rpm        int
		err        error
		wantResult string
	}{
		{name: "success", rpm: 76, wantResult: ResultSuccess},
		{name: "error", err: scheduleErr, wantResult: ResultError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			underlying := stubSchedule{requestsPerMinute: test.rpm, err: test.err}
			observer := &recordingObserver{}
			schedule, err := NewInstrumented(underlying, "uniform", observer)
			if err != nil {
				t.Fatal(err)
			}
			times := []time.Time{time.Unix(0, 0), time.Unix(0, int64(5*time.Millisecond))}
			schedule.now = func() time.Time {
				current := times[0]
				times = times[1:]
				return current
			}

			rpm, resolutionErr := schedule.RequestsPerMinute(context.Background())
			if rpm != test.rpm || !errors.Is(resolutionErr, test.err) {
				t.Fatalf("RequestsPerMinute() = (%d, %v); want (%d, %v)", rpm, resolutionErr, test.rpm, test.err)
			}
			if len(observer.observations) != 1 {
				t.Fatalf("observations = %#v; want one", observer.observations)
			}
			want := Observation{
				Type:              "uniform",
				Result:            test.wantResult,
				RequestsPerMinute: test.rpm,
				Duration:          5 * time.Millisecond,
			}
			if observer.observations[0] != want {
				t.Fatalf("observation = %#v; want %#v", observer.observations[0], want)
			}
		})
	}
}

func TestNewInstrumentedValidatesDependencies(t *testing.T) {
	t.Parallel()
	schedule := stubSchedule{}
	observer := &recordingObserver{}
	tests := []struct {
		name         string
		schedule     Schedule
		scheduleType string
		observer     Observer
	}{
		{name: "nil schedule", scheduleType: "uniform", observer: observer},
		{name: "empty schedule type", schedule: schedule, observer: observer},
		{name: "nil observer", schedule: schedule, scheduleType: "uniform"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewInstrumented(test.schedule, test.scheduleType, test.observer); err == nil {
				t.Fatal("NewInstrumented() error = nil")
			}
		})
	}
}

type stubSchedule struct {
	requestsPerMinute int
	err               error
}

func (s stubSchedule) RequestsPerMinute(context.Context) (int, error) {
	return s.requestsPerMinute, s.err
}

type recordingObserver struct {
	observations []Observation
}

func (o *recordingObserver) ObserveRateSchedule(_ context.Context, observation Observation) {
	o.observations = append(o.observations, observation)
}
