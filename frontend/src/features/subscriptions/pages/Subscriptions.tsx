import { useState } from "react";
import {
  financialActivity,
  formatActivityAmount,
  type FinancialActivity,
} from "@/features/transactions";
import { deleteTransaction } from "@/api/endpoints/transactions";
import { deleteTransfer } from "@/api/endpoints/transfers";
import Button from "@/components/ui/Button";
import Card from "@/components/ui/Card";
import DataState from "@/components/shared/DataState";
import Icon from "@/components/ui/Icon";
import PageHeader from "@/components/shared/PageHeader";
import DeleteDialog from "@/components/shared/DeleteDialog";
import { EditTransactionDialog } from "@/features/transactions";
import NewSubscriptionDialog from "../components/NewSubscriptionDialog";
import { NewTransferDialog } from "@/features/transactions";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import { formatDate } from "@/utils/finance";

export default function Subscriptions() {
  const {
    transactions,
    transfers,
    accounts,
    categories,
    settings,
    loading,
    errors,
    refresh,
  } = useFinanceData();
  const error =
    errors.transactions ??
    errors.transfers ??
    errors.accounts ??
    errors.categories ??
    errors.settings;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<FinancialActivity | null>(null);
  const [deleting, setDeleting] = useState<FinancialActivity | null>(null);
  const subscriptions = financialActivity(transactions, transfers)
    .filter((transaction) => transaction.frequency !== "one-off")
    .sort((a, b) => a.name.localeCompare(b.name));

  return (
    <div className="page-shell subscriptions-page">
      <PageHeader
        eyebrow="Recurring"
        title="Subscriptions"
        description="Your repeating transactions, all in one place."
        actions={
          <Button onClick={() => setDialogOpen(true)}>
            <Icon name="plus" />
            New subscription
          </Button>
        }
      />
      {dialogOpen && (
        <NewSubscriptionDialog
          dialogOpen={dialogOpen}
          setDialogOpen={setDialogOpen}
        />
      )}
      {editing?.type === "transfer" && (
        <NewTransferDialog
          key={editing.id}
          transfer={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {editing && editing.type !== "transfer" && (
        <EditTransactionDialog
          transaction={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          key={deleting.id}
          type={deleting.type === "transfer" ? "Transfer" : "Subscription"}
          input={deleting.name}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={async () => {
            if (deleting.type === "transfer") await deleteTransfer(deleting.id);
            else await deleteTransaction(deleting.id);
            await refresh();
          }}
        />
      )}
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading &&
        !error &&
        settings &&
        (subscriptions.length === 0 ? (
          <DataState
            loading={false}
            error={null}
            empty
            emptyTitle="No subscriptions yet"
            emptyBody="Transactions with daily, weekly, monthly or yearly frequency appear here."
          />
        ) : (
          <section className="budget-grid">
            {subscriptions.map((item) => (
              <Card className="budget-card" key={item.id}>
                <div className="budget-card-head">
                  <div>
                    <h2>{item.name}</h2>
                    <p>
                      {item.type === "transfer"
                        ? "Transfer"
                        : (categories.find(
                            (category) => category.id === item.category_id,
                          )?.name ?? "Uncategorised")}
                    </p>
                  </div>
                  <div className="record-actions">
                    <span className="pill-label">{item.frequency}</span>
                    <button
                      aria-label={`Edit ${item.name}`}
                      onClick={() => setEditing(item)}
                    >
                      <Icon name="edit" />
                    </button>
                    <button
                      aria-label={`Delete ${item.name}`}
                      onClick={() => setDeleting(item)}
                    >
                      <Icon name="trash" />
                    </button>
                  </div>
                </div>
                <p
                  className={`budget-amount ${item.type === "income" ? "positive" : ""}`}
                >
                  {formatActivityAmount(item, settings)}{" "}
                  <span>
                    /{" "}
                    {item.frequency === "daily"
                      ? "day"
                      : item.frequency === "weekly"
                        ? "week"
                        : item.frequency === "monthly"
                          ? "month"
                          : "year"}
                  </span>
                </p>
                <div className="budget-meta">
                  <span>
                    {accounts.find((account) => account.id === item.account_id)
                      ?.name ?? "Unknown account"}
                    {item.type === "transfer" && (
                      <>
                        {" "}
                        →{" "}
                        {accounts.find(
                          (account) => account.id === item.to_account_id,
                        )?.name ?? "Unknown account"}
                      </>
                    )}
                  </span>
                  <span>
                    Recorded {formatDate(item.date, settings.date_format)}
                  </span>
                </div>
              </Card>
            ))}
          </section>
        ))}
    </div>
  );
}
