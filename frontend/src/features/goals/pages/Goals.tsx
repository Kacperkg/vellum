import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { deleteGoal } from "@/api/endpoints/goals";
import Button from "@/components/ui/Button";
import Card from "@/components/ui/Card";
import DataState from "@/components/shared/DataState";
import Icon from "@/components/ui/Icon";
import PageHeader from "@/components/shared/PageHeader";
import Progress from "@/components/ui/Progress";
import NewGoalDialog from "../components/NewGoalDialog";
import DeleteDialog from "@/components/shared/DeleteDialog";
import type { Goal } from "@/api/types";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import { formatMoney } from "@/utils/finance";

export default function Goals() {
  const { goals, accounts, settings, loading, errors, refresh } =
    useFinanceData();
  const error = errors.goals ?? errors.accounts ?? errors.settings;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Goal | null>(null);
  const [deleting, setDeleting] = useState<Goal | null>(null);

  return (
    <div className="page-shell goals-page">
      <PageHeader
        eyebrow="Savings"
        title="Goals"
        description="Small contributions, compounded patiently."
        actions={
          <Button onClick={() => setDialogOpen(true)}>
            <Icon name="plus" />
            New goal
          </Button>
        }
      />
      {dialogOpen && (
        <NewGoalDialog dialogOpen={dialogOpen} setDialogOpen={setDialogOpen} />
      )}
      {editing && (
        <NewGoalDialog
          key={editing.id}
          goal={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          key={deleting.id}
          type="Goal"
          input={deleting.name}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={async () => {
            await deleteGoal(deleting.id);
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
        (goals.length === 0 ? (
          <DataState
            loading={false}
            error={null}
            empty
            emptyTitle="No goals yet"
            emptyBody="Create a savings goal and track your progress here."
          />
        ) : (
          <section className="goal-grid">
            {goals.map((goal) => {
              const progress =
                goal.target_amount > 0
                  ? (goal.saved_amount / goal.target_amount) * 100
                  : 0;
              const remaining = Math.max(
                goal.target_amount - goal.saved_amount,
                0,
              );
              const months =
                goal.monthly_contribution > 0
                  ? Math.ceil(remaining / goal.monthly_contribution)
                  : null;
              return (
                <Card className="goal-card" key={goal.id}>
                  <div className="goal-card-head">
                    <div>
                      <p className="metric-label">Goal</p>
                      <h2>{goal.name}</h2>
                    </div>
                    <div className="goal-eta">
                      <p className="metric-label">ETA</p>
                      <span>
                        {months === null
                          ? "Not set"
                          : months === 0
                            ? "Reached"
                            : `${months} mo`}
                      </span>
                      <div className="record-actions">
                        <button
                          aria-label={`Edit ${goal.name}`}
                          onClick={() => setEditing(goal)}
                        >
                          <Icon name="edit" />
                        </button>
                        <button
                          aria-label={`Delete ${goal.name}`}
                          onClick={() => setDeleting(goal)}
                        >
                          <Icon name="trash" />
                        </button>
                      </div>
                    </div>
                  </div>
                  <p className="goal-account-label">
                    {goal.account_id ? (
                      <Link to="/accounts">
                        <Icon name="accounts" />
                        {accounts.find(
                          (account) => account.id === goal.account_id,
                        )?.name ?? "Unknown account"}
                      </Link>
                    ) : (
                      "No linked account"
                    )}
                  </p>
                  <p className="goal-amount">
                    {formatMoney(goal.saved_amount, settings, 0)}{" "}
                    <span>
                      of {formatMoney(goal.target_amount, settings, 0)}
                    </span>
                  </p>
                  <Progress value={progress} />
                  <div className="milestones">
                    {[25, 50, 75, 100].map((value) => (
                      <span
                        key={value}
                        className={progress >= value ? "complete" : ""}
                      >
                        <i>{progress >= value ? "✓" : ""}</i>
                        {value}%
                      </span>
                    ))}
                  </div>
                  <div className="goal-stats">
                    <div>
                      <p className="metric-label">Saved</p>
                      <strong>
                        {formatMoney(goal.saved_amount, settings, 0)}
                      </strong>
                    </div>
                    <div>
                      <p className="metric-label">Remaining</p>
                      <strong>{formatMoney(remaining, settings, 0)}</strong>
                    </div>
                    <div>
                      <p className="metric-label">Monthly</p>
                      <strong>
                        {formatMoney(goal.monthly_contribution, settings, 0)}
                      </strong>
                    </div>
                  </div>
                </Card>
              );
            })}
          </section>
        ))}
    </div>
  );
}
