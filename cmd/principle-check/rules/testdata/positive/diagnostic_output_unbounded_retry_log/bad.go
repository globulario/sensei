// Positive-control fixture for diagnostic_output_unbounded_retry_log.
// logger.Error + time.Sleep as direct siblings inside an if-err block.
package badfix

import "time"

type loggerT struct{}

func (loggerT) Error(args ...any) {}

var logger loggerT

func eventLoop(connect func() error) {
	for {
		err := connect()
		if err != nil {
			logger.Error("event service connection failed", "err", err) // BAD
			time.Sleep(10 * time.Second)
			continue
		}
	}
}
