import { useId, useState, type FormEvent } from "react";
import { createBudget, updateBudget } from "@/api/endpoints/budgets";
import type { Budget } from "@/api/types";
import { apiErrorMessage } from "@/api/errors";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import { majorToMinor, minorToMajor } from "@/utils/finance";
import Input from "@/components/ui/Input";
import Select from "@/components/ui/Select";
import Dialog from "@/components/ui/Dialog";
import Button from "@/components/ui/Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  budget?: Budget;
};

export default function NewBudgetDialog({
  dialogOpen,
  setDialogOpen,
  budget,
}: DialogProps) {
  const formId = useId();
  const { budgets, categories, refresh } = useFinanceData();
  const [categoryId, setCategoryId] = useState(budget?.category_id ?? "");
  const [amount, setAmount] = useState(
    budget ? String(minorToMajor(budget.allocated_amount)) : "",
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setError("");
    try {
      const request = {
        category_id: categoryId,
        allocated_amount: majorToMinor(amount),
      };
      if (budget) await updateBudget(budget.id, request);
      else await createBudget(request);
      await refresh();
      setAmount("");
      setCategoryId("");
      setDialogOpen(false);
    } catch (error) {
      setError(
        error instanceof RangeError
          ? error.message
          : apiErrorMessage(error, "The budget could not be saved."),
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
      title={budget ? "Edit Budget" : "New Budget"}
      description="Allocate a monthly limit to a category."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            disabled={
              saving ||
              categories.every((category) =>
                budgets.some(
                  (item) =>
                    item.category_id === category.id && item.id !== budget?.id,
                ),
              )
            }
          >
            {saving ? "Saving…" : budget ? "Save changes" : "Create budget"}
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
          <label htmlFor={`${formId}-category`}>Category</label>
          <Select
            id={`${formId}-category`}
            required
            value={categoryId}
            onChange={(event) => setCategoryId(event.target.value)}
          >
            <option value="" disabled>
              Select a category
            </option>
            {categories
              .filter(
                (category) =>
                  !budgets.some(
                    (item) =>
                      item.category_id === category.id &&
                      item.id !== budget?.id,
                  ),
              )
              .map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
          </Select>
        </div>
        <div className="flex flex-col gap-2">
          <label htmlFor={`${formId}-amount`}>Monthly allocation</label>
          <Input
            id={`${formId}-amount`}
            required
            variant="number"
            type="text"
            inputMode="decimal"
            placeholder="500"
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
          />
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
