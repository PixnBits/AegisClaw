import { StageStatus } from '@/contracts';
import { defaultStages, slugStage } from '@/lib/harness';
import { useIsMobile } from '@/hooks/useMediaQuery';
import './CompactHarness.css';

type Props = {
  stages?: StageStatus[];
};

export function PipelineStrip({ stages }: Props) {
  const isMobile = useIsMobile();
  const list = stages?.length ? stages : defaultStages();

  return (
    <div
      className="pipeline-strip"
      data-testid="pipeline-strip"
      tabIndex={isMobile ? 0 : undefined}
      role={isMobile ? 'region' : undefined}
      aria-label={isMobile ? 'Pipeline stages' : undefined}
    >
      {list.map((stage) => (
        <div
          key={stage.name}
          className={`pipeline-stage pipeline-stage--${stage.status || 'pending'}`}
          data-testid={`pipeline-stage-${slugStage(stage.name)}`}
        >
          <span className="pipeline-stage__name">{stage.name}</span>
        </div>
      ))}
    </div>
  );
}