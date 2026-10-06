type Props = {
  idValid?: boolean;
  idError?: string;
};

export function InvalidChannelBadge({ idValid, idError }: Props) {
  if (idValid !== false) return null;
  return (
    <span className="channel-invalid-id-badge" title={idError || 'invalid id'} data-testid="invalid-id-badge">
      invalid id
    </span>
  );
}
