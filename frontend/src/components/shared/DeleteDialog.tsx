import Dialog from "@/components/ui/Dialog";
import Button from "@/components/ui/Button";
import { useState } from "react";
import { apiErrorMessage } from "@/api/errors";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  type: keyof typeof deleteTypes;
  input: string;
  onConfirm?: () => Promise<void>;
};

const deleteTypes = {
  Transfer: {
    description: (input: string) =>
      `This will remove ${input} and reverse its recorded effect on both account balances.`,
  },
  Transaction: {
    description: (input: string) =>
      `This will remove ${input} from your history. This can't be undone.`,
  },
  Budget: {
    description: (input: string) =>
      `This will remove the ${input} budget. Your transactions will remain.`,
  },
  Goal: {
    description: (input: string) => `This will remove ${input} goal.`,
  },
  Subscription: {
    description: (input: string) =>
      `This will delete the recurring transaction ${input} from Vellum.`,
  },
  Account: {
    description: (input: string) =>
      `This will remove the account ${input}. Move or remove its transactions and transfers, and move or unlink its goals, before deleting it.`,
  },
};

export default function DeleteDialog({
  dialogOpen,
  setDialogOpen,
  type,
  input,
  onConfirm,
}: DialogProps) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function handleDelete() {
    if (!onConfirm || saving) return;
    setSaving(true);
    setError("");
    try {
      await onConfirm();
      setDialogOpen(false);
    } catch (error) {
      setError(
        apiErrorMessage(
          error,
          `The ${type.toLowerCase()} could not be deleted. Please try again.`,
        ),
      );
    } finally {
      setSaving(false);
    }
  }
  return (
    <Dialog
      busy={saving}
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title={"Delete " + type}
      description={deleteTypes[type].description(input)}
      headerBorder={false}
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={!onConfirm || saving}
            onClick={() => void handleDelete()}
          >
            {saving ? "Deleting…" : "Delete"}
          </Button>
        </>
      }
    >
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </Dialog>
  );
}
