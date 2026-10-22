import { useMemo, useState } from "react";
import Button from "../components/Button";
import Card from "../components/Card";
import DataState from "../components/DataState";
import Icon from "../components/Icon";
import PageHeader from "../components/PageHeader";
import Progress from "../components/Progress";
import NewBudgetDialog from "../components/dialog/NewBudgetDialog";
import DeleteDialog from "../components/dialog/DeleteDialog";
import { deleteBudget } from "../api/endpoints/budgets";
import type { Budget } from "../api/types";
import { useFinanceData } from "../context/FinanceDataContext";
import { formatMoney, isCurrentMonth } from "../lib/finance";

export default function Budgets() {
  const {
    budgets,
    categories,
    transactions,
    settings,
    loading,
    errors,
    refresh,
  } = useFinanceData();
  const error =
    errors.budgets ??
    errors.categories ??
    errors.transactions ??
    errors.settings;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Budget | null>(null);
  const [deleting, setDeleting] = useState<Budget | null>(null);
  const categoryById = useMemo(
    () => new Map(categories.map((item) => [item.id, item.name])),
    [categories],
  );
  const spentByCategory = useMemo(() => {
    const totals = new Map<string, number>();
    transactions
      .filter((item) => item.type === "expense" && isCurrentMonth(item.date))
      .forEach((item) =>
        totals.set(
          item.category_id,
          (totals.get(item.category_id) ?? 0) + item.amount,
        ),
      );
    return totals;
  }, [transactions]);
  const allocated = budgets.reduce(
    (sum, item) => sum + item.allocated_amount,
    0,
  );
  const budgetCategoryIds = new Set(budgets.map((item) => item.category_id));
  const spent = [...spentByCategory.entries()]
    .filter(([categoryId]) => budgetCategoryIds.has(categoryId))
    .reduce((sum, [, amount]) => sum + amount, 0);
  const utilisation = allocated > 0 ? (spent / allocated) * 100 : 0;
  const monthLabel = new Intl.DateTimeFormat("en", {
    month: "long",
    year: "numeric",
  }).format(new Date());

  return (
    <div className="page-shell budgets-page">
      <PageHeader
        eyebrow={monthLabel}
        title="Budgets"
        description="Calmly allocated. Quietly tracked."
        actions={
          <Button onClick={() => setDialogOpen(true)}>
            <Icon name="plus" />
            New budget
          </Button>
        }
      />
      {dialogOpen && (
        <NewBudgetDialog
          dialogOpen={dialogOpen}
          setDialogOpen={setDialogOpen}
        />
      )}
      {editing && (
        <NewBudgetDialog
          key={editing.id}
          budget={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          key={deleting.id}
          type="Budget"
          input={categoryById.get(deleting.category_id) ?? "Category"}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={async () => {
            await deleteBudget(deleting.id);
            await refresh();
          }}
        />
      )}
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading && !error && settings && (
        <>
          <Card className="budget-summary">
            <div>
              <p className="metric-label">Allocated</p>
              <strong>{formatMoney(allocated, settings, 0)}</strong>
            </div>
            <div>
              <p className="metric-label">Spent</p>
              <strong>{formatMoney(spent, settings, 0)}</strong>
            </div>
            <div>
              <p className="metric-label">Utilisation</p>
              <strong>{Math.round(utilisation)}%</strong>
              <Progress value={utilisation} />
            </div>
          </Card>
          {budgets.length === 0 ? (
            <div className="budget-empty-state">
              <DataState
                loading={false}
                error={null}
                empty
                emptyTitle="No budgets yet"
                emptyBody="Create a category budget to compare it with this month’s spending."
              />
            </div>
          ) : (
            <section className="budget-grid">
              {budgets.map((budget) => {
                const budgetSpent =
                  spentByCategory.get(budget.category_id) ?? 0;
                const used =
                  budget.allocated_amount > 0
                    ? (budgetSpent / budget.allocated_amount) * 100
                    : 0;
                return (
                  <Card className="budget-card" key={budget.id}>
                    <div className="budget-card-head">
                      <div>
                        <h2>
                          {categoryById.get(budget.category_id) ??
                            "Uncategorised"}
                        </h2>
                        <p>
                          {formatMoney(budget.allocated_amount, settings)}{" "}
                          allocated
                        </p>
                      </div>
                      <div className="record-actions">
                        <span className="pill-label">
                          {used >= 100
                            ? "Over"
                            : used >= 80
                              ? "Close"
                              : "On track"}
                        </span>
                        <button
                          aria-label={`Edit ${categoryById.get(budget.category_id) ?? "budget"}`}
                          onClick={() => setEditing(budget)}
                        >
                          <Icon name="edit" />
                        </button>
                        <button
                          aria-label={`Delete ${categoryById.get(budget.category_id) ?? "budget"}`}
                          onClick={() => setDeleting(budget)}
                        >
                          <Icon name="trash" />
                        </button>
                      </div>
                    </div>
                    <p className="budget-amount">
                      {formatMoney(budgetSpent, settings)}{" "}
                      <span>
                        of {formatMoney(budget.allocated_amount, settings)}
                      </span>
                    </p>
                    <Progress value={used} tone="neutral" />
                    <div className="budget-meta">
                      <span>{Math.round(used)}% used</span>
                      <span>
                        {formatMoney(
                          Math.max(budget.allocated_amount - budgetSpent, 0),
                          settings,
                        )}{" "}
                        left
                      </span>
                    </div>
                  </Card>
                );
              })}
            </section>
          )}
        </>
      )}
    </div>
  );
}
