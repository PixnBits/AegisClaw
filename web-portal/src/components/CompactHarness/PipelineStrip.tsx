import { StageStatus } from '@/contracts';
import { defaultStages, slugStage } from '@/lib/harness';
import './CompactHarness.css';

type Props = {
  stages?: StageStatus[];
};

export function PipelineStrip({ stages }: Props) {
  const list = stages?.length ? stages : defaultStages();

  return (
    <div className="pipeline-strip" data-testid="pipeline-strip">
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