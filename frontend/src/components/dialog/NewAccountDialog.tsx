import { useId, useState, type FormEvent } from "react";
import { createAccount, updateAccount } from "../../api/endpoints/accounts";
import type { Account } from "../../api/types";
import { apiErrorMessage } from "../../api/errors";
import { useFinanceData } from "../../context/FinanceDataContext";
import Input from "../Input";
import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  account?: Account;
};

export default function NewAccountDialog({
  dialogOpen,
  setDialogOpen,
  account,
}: DialogProps) {
  const formId = useId();
  const { refresh } = useFinanceData();
  const [name, setName] = useState(account?.name ?? "");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setError("");
    try {
      if (account) await updateAccount(account.id, { name: name.trim() });
      else await createAccount({ name: name.trim() });
      await refresh();
      setName("");
      setDialogOpen(false);
    } catch (error) {
      setError(apiErrorMessage(error, "The account could not be saved."));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      busy={saving}
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title={account ? "Edit Account" : "New Account"}
      description="Give this account a name so you can attach transactions to it."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button type="submit" form={formId} disabled={saving}>
            {saving ? "Saving…" : account ? "Save changes" : "Add account"}
          </Button>
        </>
      }
    >
      <form id={formId} className="finance-form" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-2">
          <label htmlFor={`${formId}-name`}>Name</label>
          <Input
            id={`${formId}-name`}
            required
            placeholder="Monzo Savings"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
      </form>
    </Dialog>
  );
}
