package dashboard

import (
	"context"
	"time"
)

func (s *Server) startMonitoringPublisher() {
	s.initSTOMP()
	ctx := s.bgCtx
	go func() {
		publish := func() {
			ctx, cancel := context.WithTimeout(context.Background(), spaAPITimeout)
			data := s.collectMonitoringSPA(ctx)
			cancel()
			s.stompPublisher().PublishMonitoringStats(data)
		}
		publish()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		if ctx == nil {
			for range ticker.C {
				publish()
			}
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				publish()
			}
		}
	}()
}
