import Button from "./Button";

type DataStateProps = {
  loading: boolean;
  error: string | null;
  empty?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onRetry?: () => void;
};

export default function DataState({
  loading,
  error,
  empty = false,
  emptyTitle = "Nothing here yet",
  emptyBody = "Add your first item to get started.",
  onRetry,
}: DataStateProps) {
  if (loading) {
    return <div className="data-state">Loading your finance data…</div>;
  }
  if (error) {
    return (
      <div className="data-state" role="alert">
        <strong>Unable to load data</strong>
        <p>{error}</p>
        {onRetry && <Button onClick={onRetry}>Try again</Button>}
      </div>
    );
  }
  if (empty) {
    return (
      <div className="data-state">
        <strong>{emptyTitle}</strong>
        <p>{emptyBody}</p>
      </div>
    );
  }
  return null;
}
