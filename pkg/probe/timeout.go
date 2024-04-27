package probe

import (
	"fmt"
	"strconv"
	"time"

	"github.com/go-kit/log/level"
)

func (p *Probe) getProbeTimeout() time.Duration {
	var (
		err     error
		timeout int64
	)

	if v := p.request.Header.Get("X-Prometheus-Scrape-Timeout-Seconds"); v != "" {
		timeout, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			_ = level.Warn(p.logger).Log("msg", fmt.Sprintf("Couldn't parse X-Prometheus-Scrape-Timeout-Seconds: %q. Defaulting timeout to %d", v, 10))
		}
	}

	if timeout == 0 {
		timeout = 10
	}

	timeout = timeout*1000 - 500 // Subtract 0.5s to give some buffer for the context deadline

	return time.Duration(timeout) * time.Millisecond
}
