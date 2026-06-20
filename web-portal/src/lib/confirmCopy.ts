import type { ProposalAction } from '@/api/client';

/** Runtime guard for court proposal actions (bridge allow-list). */
export function isProposalAction(value: string): value is ProposalAction {
  return value === 'approve' || value === 'reject' || value === 'defer';
}

export const PROPOSAL_ACTION_LABELS: Record<ProposalAction, string> = {
  approve: 'Approve',
  reject: 'Reject',
  defer: 'Defer',
};

export function proposalActionConfirmCopy(action: ProposalAction, title: string): { title: string; message: string } {
  switch (action) {
    case 'approve':
      return {
        title: 'Approve proposal?',
        message: `Approve "${title}"? The Court will record this decision and may advance related work.`,
      };
    case 'reject':
      return {
        title: 'Reject proposal?',
        message: `Reject "${title}"? This records a negative Court outcome for this proposal.`,
      };
    case 'defer':
      return {
        title: 'Defer proposal?',
        message: `Defer "${title}"? Review will be postponed until you revisit this proposal.`,
      };
  }
}

export function exportProposalConfirmCopy(title: string): { title: string; message: string } {
  return {
    title: 'Export proposal report?',
    message: `Download a report for "${title}"? The export may include governance rationale and review details.`,
  };
}

export type AgentControlAction = 'pause' | 'resume' | 'cancel';

export function agentActionConfirmCopy(
  action: AgentControlAction,
  agentId: string,
): { title: string; message: string; variant: 'primary' | 'danger' } {
  switch (action) {
    case 'pause':
      return {
        title: 'Pause agent?',
        message: `Pause ${agentId}? In-flight work will stop until you resume the agent.`,
        variant: 'primary',
      };
    case 'resume':
      return {
        title: 'Resume agent?',
        message: `Resume ${agentId}? The agent will continue from its paused state.`,
        variant: 'primary',
      };
    case 'cancel':
      return {
        title: 'Cancel agent run?',
        message: `Cancel ${agentId}? This stops the current run and cannot be undone.`,
        variant: 'danger',
      };
  }
}
