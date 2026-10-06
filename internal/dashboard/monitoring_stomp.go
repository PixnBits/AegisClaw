package dashboard

import (
	"context"
	"time"
)

func (s *Server) startMonitoringPublisher(ctx context.Context) {
	s.initSTOMP()
	if ctx == nil {
		return
	}
	s.goBackground(func() {
		publish := func() {
			pctx, cancel := context.WithTimeout(ctx, spaAPITimeout)
			defer cancel()
			data := s.collectMonitoringSPA(pctx)
			s.stompPublisher().PublishMonitoringStats(data)
		}
		publish()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				publish()
			}
		}
	})
}
