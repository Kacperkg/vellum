import { useId, useState, type FormEvent } from "react";
import { createGoal, updateGoal } from "@/api/endpoints/goals";
import type { Goal } from "@/api/types";
import { apiErrorMessage } from "@/api/errors";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import {
  formatMoney,
  majorToMinor,
  minorToMajor,
  moneyInteger,
} from "@/utils/finance";
import Input from "@/components/ui/Input";
import Select from "@/components/ui/Select";
import Dialog from "@/components/ui/Dialog";
import Button from "@/components/ui/Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  goal?: Goal;
  initialAccountId?: string;
};

export default function NewGoalDialog({
  dialogOpen,
  setDialogOpen,
  goal,
  initialAccountId,
}: DialogProps) {
  const formId = useId();
  const { refresh, accounts, settings, errors, loading } = useFinanceData();
  const [accountId, setAccountId] = useState(
    goal?.account_id ?? initialAccountId ?? "",
  );
  const [name, setName] = useState(goal?.name ?? "");
  const [target, setTarget] = useState(
    goal ? String(minorToMajor(goal.target_amount)) : "",
  );
  const [saved, setSaved] = useState(
    goal ? String(minorToMajor(goal.saved_amount)) : "0",
  );
  const [monthly, setMonthly] = useState(
    goal ? String(minorToMajor(goal.monthly_contribution)) : "0",
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || loading || errors.accounts) return;
    setSaving(true);
    setError("");
    try {
      const request = {
        name: name.trim(),
        target_amount: majorToMinor(target),
        saved_amount: majorToMinor(saved, true),
        monthly_contribution: majorToMinor(monthly, true),
        account_id: accountId || null,
      };
      if (request.saved_amount > request.target_amount)
        throw new RangeError("Saved amount cannot exceed the goal target.");
      if (goal) await updateGoal(goal.id, request);
      else await createGoal(request);
      await refresh();
      setName("");
      setTarget("");
      setSaved("0");
      setMonthly("0");
      setDialogOpen(false);
    } catch (error) {
      setError(
        error instanceof RangeError
          ? error.message
          : apiErrorMessage(error, "The goal could not be saved."),
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
      title={goal ? "Edit Goal" : "New Goal"}
      description="Set a target and track it patiently."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            disabled={saving || loading || !!errors.accounts}
          >
            {saving ? "Saving…" : goal ? "Save changes" : "Create goal"}
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className="finance-form flex flex-col gap-4"
        onSubmit={handleSubmit}
      >
        <div className="flex flex-col gap-2">
          <label htmlFor={`${formId}-name`}>Name</label>
          <Input
            id={`${formId}-name`}
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Emergency Fund"
          />
        </div>
        <div className="flex gap-4 items-center">
          <div className="flex flex-col gap-2 w-full">
            <label htmlFor={`${formId}-target`}>Target</label>
            <Input
              id={`${formId}-target`}
              required
              variant="number"
              type="text"
              inputMode="decimal"
              value={target}
              onChange={(event) => setTarget(event.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2 w-full">
            <label htmlFor={`${formId}-saved`}>Saved so far</label>
            <Input
              id={`${formId}-saved`}
              required
              variant="number"
              type="text"
              inputMode="decimal"
              value={saved}
              onChange={(event) => setSaved(event.target.value)}
            />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <label htmlFor={`${formId}-monthly`}>Monthly contribution</label>
          <Input
            id={`${formId}-monthly`}
            required
            variant="number"
            type="text"
            inputMode="decimal"
            value={monthly}
            onChange={(event) => setMonthly(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <label htmlFor={`${formId}-account`}>Linked account</label>
          <Select
            id={`${formId}-account`}
            value={accountId}
            disabled={loading || !!errors.accounts}
            onChange={(event) => setAccountId(event.target.value)}
          >
            <option value="">No linked account</option>
            {accounts.map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
          </Select>
          <p className="text-sm text-muted-foreground">
            Linking allocates the full target to the selected account. It does
            not move money or change your saved progress.
          </p>
          {accountId &&
            settings &&
            !errors.accounts &&
            (() => {
              const account = accounts.find((item) => item.id === accountId);
              if (!account) return null;
              let nextTarget: number;
              try {
                nextTarget = majorToMinor(target);
              } catch {
                return null;
              }
              const available =
                moneyInteger(account.unallocated_amount) +
                moneyInteger(
                  goal?.account_id === accountId ? goal.target_amount : 0,
                ) -
                moneyInteger(nextTarget);
              return (
                <p
                  aria-live="polite"
                  className={`text-sm ${available < 0 ? "text-destructive" : "text-muted-foreground"}`}
                >
                  {available < 0
                    ? `${formatMoney(-available, settings)} short of covering linked goal targets.`
                    : `${formatMoney(available, settings)} unallocated after this goal.`}
                </p>
              );
            })()}
          {errors.accounts && (
            <p role="alert" className="text-sm text-destructive">
              Accounts could not be loaded. Close this form and retry the page
              before saving.
            </p>
          )}
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
