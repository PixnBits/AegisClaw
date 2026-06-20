import { ReasoningPolicy } from '@/contracts';
import { usePolicyStore } from '@/store/policyStore';
import './PolicyPreset.css';

const PRESETS: { id: ReasoningPolicy; label: string; description: string }[] = [
  { id: 'progressive', label: 'Progressive', description: 'Live expanded, post-decision collapsed' },
  { id: 'paranoid', label: 'Paranoid', description: 'All reasoning expanded' },
  { id: 'velocity', label: 'Velocity', description: 'Collapsed except live steps' },
];

type Props = {
  channelId?: string;
  showInheritance?: boolean;
};

export function PolicyPresetToggle({ channelId, showInheritance }: Props) {
  const globalPolicy = usePolicyStore((s) => s.globalPolicy);
  const channelPolicies = usePolicyStore((s) => s.channelPolicies);
  const enterpriseLocked = usePolicyStore((s) => s.enterpriseLocked);
  const setGlobal = usePolicyStore((s) => s.setGlobalPolicy);
  const setChannel = usePolicyStore((s) => s.setChannelPolicy);
  const effective = channelId ? usePolicyStore.getState().effectivePolicy(channelId) : globalPolicy;

  const setPolicy = (policy: ReasoningPolicy) => {
    if (channelId) setChannel(channelId, policy);
    else setGlobal(policy);
  };

  return (
    <div className="policy-preset" data-testid="policy-preset-toggle" role="group" aria-label="Reasoning visibility policy">
      {enterpriseLocked && (
        <p className="policy-preset__lock subtle" data-testid="policy-enterprise-lock">
          Policy locked by enterprise administrator
        </p>
      )}
      <div className="policy-preset__segments">
        {PRESETS.map((preset) => (
          <button
            key={preset.id}
            type="button"
            className={`policy-preset__segment${effective === preset.id ? ' is-active' : ''}`}
            data-testid={`policy-${preset.id}`}
            disabled={enterpriseLocked}
            aria-pressed={effective === preset.id}
            title={preset.description}
            onClick={() => setPolicy(preset.id)}
          >
            {preset.label}
          </button>
        ))}
      </div>
      {showInheritance && channelId && channelPolicies[channelId] && (
        <p className="subtle">Channel override — global: {globalPolicy}</p>
      )}
    </div>
  );
}