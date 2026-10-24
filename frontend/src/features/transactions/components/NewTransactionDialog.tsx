import { useId, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import {
  createTransaction,
  updateTransaction,
} from "@/api/endpoints/transactions";
import { createTransfer, updateTransfer } from "@/api/endpoints/transfers";
import { apiErrorMessage } from "@/api/errors";
import type {
  Transaction,
  TransactionFrequency,
  TransactionType,
  Transfer,
} from "@/api/types";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import { recordedAccountBalance } from "../utils/activity";
import { formatMoney, majorToMinor, minorToMajor } from "@/utils/finance";
import Input from "@/components/ui/Input";
import Select from "@/components/ui/Select";
import Dialog from "@/components/ui/Dialog";
import Button from "@/components/ui/Button";

type FormType = TransactionType | "transfer";
type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  subscriptionMode?: boolean;
  transaction?: Transaction;
  transfer?: Transfer;
  initialType?: FormType;
};

export default function NewTransactionDialog({
  dialogOpen,
  setDialogOpen,
  subscriptionMode = false,
  transaction,
  transfer,
  initialType = "expense",
}: DialogProps) {
  const formId = useId();
  const fieldId = (name: string) => `${formId}-${name}`;
  const {
    accounts,
    categories,
    transactions,
    transfers,
    settings,
    errors,
    loading,
    refresh,
  } = useFinanceData();
  const original = transaction ?? transfer;
  const [type, setType] = useState<FormType>(
    transfer ? "transfer" : (transaction?.type ?? initialType),
  );
  const [name, setName] = useState(
    original?.name ?? (initialType === "transfer" ? "Account transfer" : ""),
  );
  const [amount, setAmount] = useState(
    original ? String(minorToMajor(original.amount)) : "",
  );
  const [date, setDate] = useState(() => {
    if (original) return original.date.slice(0, 10);
    const today = new Date();
    return `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
  });
  const [categoryId, setCategoryId] = useState(transaction?.category_id ?? "");
  const [accountId, setAccountId] = useState(
    transfer?.from_account_id ?? transaction?.account_id ?? "",
  );
  const [toAccountId, setToAccountId] = useState(transfer?.to_account_id ?? "");
  const [frequency, setFrequency] = useState<TransactionFrequency>(
    original?.frequency ?? (subscriptionMode ? "monthly" : "one-off"),
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const isTransfer = type === "transfer";
  const resourceError =
    errors.accounts ?? (isTransfer ? errors.transfers : errors.categories);
  const unavailable =
    loading ||
    !!resourceError ||
    accounts.length < (isTransfer ? 2 : 1) ||
    (!isTransfer && categories.length === 0);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || unavailable) return;
    if (isTransfer && accountId === toAccountId) {
      setError("Choose two different accounts.");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const common = {
        name: name.trim(),
        amount: majorToMinor(Number(amount)),
        date:
          original && date === original.date.slice(0, 10)
            ? original.date
            : new Date(`${date}T12:00:00Z`).toISOString(),
        frequency,
      };
      if (type === "transfer") {
        const request = {
          ...common,
          from_account_id: accountId,
          to_account_id: toAccountId,
        };
        if (transfer) await updateTransfer(transfer.id, request);
        else await createTransfer(request);
      } else {
        const request = {
          ...common,
          account_id: accountId,
          category_id: categoryId,
          type,
        };
        if (transaction) await updateTransaction(transaction.id, request);
        else await createTransaction(request);
      }
      await refresh();
      setDialogOpen(false);
    } catch (error) {
      setError(
        apiErrorMessage(
          error,
          `The ${isTransfer ? "transfer" : "transaction"} could not be saved. Please try again.`,
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
      title={
        original
          ? isTransfer
            ? "Edit Transfer"
            : "Edit Transaction"
          : subscriptionMode
            ? "New Subscription"
            : "New Transaction"
      }
      description={
        isTransfer
          ? "Move money between your accounts without changing your overall income or spending."
          : "Log income, an expense or a transfer between accounts."
      }
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button type="submit" form={formId} disabled={saving || unavailable}>
            {saving
              ? "Saving…"
              : original
                ? "Save changes"
                : isTransfer
                  ? "Record transfer"
                  : subscriptionMode
                    ? "Create subscription"
                    : "Add transaction"}
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className="finance-form flex flex-col w-full gap-4"
        onSubmit={handleSubmit}
      >
        <div className="w-full flex gap-4">
          <div className="w-full flex flex-col gap-2">
            <label htmlFor={fieldId("type")}>Type</label>
            <Select
              id={fieldId("type")}
              value={type}
              onChange={(event) => {
                setType(event.target.value as FormType);
                setError("");
              }}
            >
              <option value="income" disabled={!!transfer}>
                Income
              </option>
              <option value="expense" disabled={!!transfer}>
                Expense
              </option>
              <option value="transfer" disabled={!!transaction}>
                Transfer
              </option>
            </Select>
          </div>
          <div className="w-full flex flex-col gap-2">
            <label htmlFor={fieldId("frequency")}>Frequency</label>
            <Select
              id={fieldId("frequency")}
              value={frequency}
              onChange={(event) =>
                setFrequency(event.target.value as TransactionFrequency)
              }
            >
              {!subscriptionMode && <option value="one-off">One-off</option>}
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
              <option value="monthly">Monthly</option>
              <option value="yearly">Yearly</option>
            </Select>
          </div>
        </div>
        {resourceError && (
          <p role="alert" className="text-sm text-destructive">
            Your{" "}
            {isTransfer ? "accounts or transfers" : "accounts or categories"}{" "}
            could not be loaded. Close this form and retry loading the page.
          </p>
        )}
        {unavailable && !resourceError && !loading && (
          <p className="text-sm text-muted-foreground">
            {isTransfer
              ? "Add at least two accounts"
              : "Create an account and ensure categories are available"}{" "}
            in{" "}
            <Link
              to="/settings"
              onClick={() => setDialogOpen(false)}
              className="underline"
            >
              Settings
            </Link>{" "}
            before recording {isTransfer ? "transfers" : "transactions"}.
          </p>
        )}
        <div className="w-full flex flex-col gap-2">
          <label htmlFor={fieldId("name")}>
            {isTransfer ? "Description" : "Name"}
          </label>
          <Input
            id={fieldId("name")}
            required
            placeholder={
              isTransfer ? "e.g. Monthly bills funding" : "e.g. Whole Foods"
            }
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className="flex w-full gap-4">
          <div className="w-full flex flex-col gap-2">
            <label htmlFor={fieldId("amount")}>Amount</label>
            <Input
              id={fieldId("amount")}
              required
              min="0.01"
              step="0.01"
              variant="number"
              value={amount}
              onChange={(event) => setAmount(event.target.value)}
            />
          </div>
          <div className="w-full flex flex-col gap-2">
            <label htmlFor={fieldId("date")}>Date</label>
            <Input
              id={fieldId("date")}
              required
              variant="date"
              value={date}
              onChange={(event) => setDate(event.target.value)}
            />
          </div>
        </div>
        <div className="w-full flex gap-4">
          <div className="w-full flex flex-col gap-2">
            <label htmlFor={fieldId("account")}>
              {isTransfer ? "From account" : "Account"}
            </label>
            <Select
              id={fieldId("account")}
              required
              value={accountId}
              onChange={(event) => {
                setAccountId(event.target.value);
                if (toAccountId === event.target.value) setToAccountId("");
              }}
            >
              <option value="" disabled>
                {isTransfer ? "Select source" : "Select an account"}
              </option>
              {accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </Select>
          </div>
          {isTransfer ? (
            <div className="w-full flex flex-col gap-2">
              <label htmlFor={fieldId("to-account")}>To account</label>
              <Select
                id={fieldId("to-account")}
                required
                value={toAccountId}
                onChange={(event) => setToAccountId(event.target.value)}
              >
                <option value="" disabled>
                  Select destination
                </option>
                {accounts
                  .filter((account) => account.id !== accountId)
                  .map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
              </Select>
            </div>
          ) : (
            <div className="w-full flex flex-col gap-2">
              <label htmlFor={fieldId("category")}>Category</label>
              <Select
                id={fieldId("category")}
                required
                value={categoryId}
                onChange={(event) => setCategoryId(event.target.value)}
              >
                <option value="" disabled>
                  Select a category
                </option>
                {categories.map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
              </Select>
            </div>
          )}
        </div>
        {isTransfer &&
          accountId &&
          settings &&
          !errors.transactions &&
          !errors.transfers && (
            <p className="text-sm text-muted-foreground">
              Source recorded balance:{" "}
              {formatMoney(
                recordedAccountBalance(accountId, transactions, transfers),
                settings,
              )}
            </p>
          )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
