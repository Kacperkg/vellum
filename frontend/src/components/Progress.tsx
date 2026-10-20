type ProgressProps = {
  value: number;
  tone?: "primary" | "neutral" | "positive";
  className?: string;
};

export default function Progress({
  value,
  tone = "primary",
  className = "",
}: ProgressProps) {
  return (
    <div
      className={`progress-track ${className}`}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(value)}
    >
      <span
        className={`progress-fill progress-${tone}`}
        style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
      />
    </div>
  );
}
