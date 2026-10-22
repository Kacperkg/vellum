import { useState } from "react";
import { deleteAccount } from "../api/endpoints/accounts";
import type { Account, Goal } from "../api/types";
import Button from "../components/Button";
import Card from "../components/Card";
import DataState from "../components/DataState";
import Icon from "../components/Icon";
import PageHeader from "../components/PageHeader";
import Progress from "../components/Progress";
import DeleteDialog from "../components/dialog/DeleteDialog";
import NewAccountDialog from "../components/dialog/NewAccountDialog";
import NewGoalDialog from "../components/dialog/NewGoalDialog";
import NewTransferDialog from "../components/dialog/NewTransferDialog";
import { useFinanceData } from "../context/FinanceDataContext";
import { formatMoney } from "../lib/finance";

export default function Accounts() {
  const { accounts, goals, settings, loading, errors, refresh } =
    useFinanceData();
  const error = errors.accounts ?? errors.goals ?? errors.settings;
  const [accountOpen, setAccountOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [editing, setEditing] = useState<Account | null>(null);
  const [deleting, setDeleting] = useState<Account | null>(null);
  const [goalAccountId, setGoalAccountId] = useState<string | null>(null);
  const [editingGoal, setEditingGoal] = useState<Goal | null>(null);
  const totalBalance = accounts.reduce(
    (sum, account) => sum + account.balance,
    0,
  );
  const totalAllocated = accounts.reduce(
    (sum, account) => sum + account.allocated_amount,
    0,
  );
  const totalUnallocated = accounts.reduce(
    (sum, account) => sum + account.unallocated_amount,
    0,
  );

  return (
    <div className="page-shell accounts-page">
      <PageHeader
        eyebrow="Your money"
        title="Accounts"
        description="Every account, with a clear view of what your goals set aside."
        actions={
          <>
            <Button variant="outline" onClick={() => setTransferOpen(true)}>
              <Icon name="transactions" />
              Transfer
            </Button>
            <Button onClick={() => setAccountOpen(true)}>
              <Icon name="plus" />
              Add account
            </Button>
          </>
        }
      />
      {accountOpen && (
        <NewAccountDialog dialogOpen setDialogOpen={setAccountOpen} />
      )}
      {transferOpen && (
        <NewTransferDialog dialogOpen setDialogOpen={setTransferOpen} />
      )}
      {editing && (
        <NewAccountDialog
          key={editing.id}
          account={editing}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          key={deleting.id}
          type="Account"
          input={deleting.name}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setDeleting(null);
          }}
          onConfirm={async () => {
            await deleteAccount(deleting.id);
            await refresh();
          }}
        />
      )}
      {goalAccountId && (
        <NewGoalDialog
          initialAccountId={goalAccountId}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setGoalAccountId(null);
          }}
        />
      )}
      {editingGoal && (
        <NewGoalDialog
          key={editingGoal.id}
          goal={editingGoal}
          dialogOpen
          setDialogOpen={(open) => {
            if (!open) setEditingGoal(null);
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
        (accounts.length === 0 ? (
          <DataState
            loading={false}
            error={null}
            empty
            emptyTitle="No accounts yet"
            emptyBody="Add an account to track its balance and link your savings goals."
          />
        ) : (
          <>
            <Card className="budget-summary account-summary">
              <div>
                <p className="metric-label">Total recorded balance</p>
                <strong>{formatMoney(totalBalance, settings)}</strong>
              </div>
              <div>
                <p className="metric-label">Allocated to goals</p>
                <strong>{formatMoney(totalAllocated, settings)}</strong>
              </div>
              <div>
                <p className="metric-label">
                  {totalUnallocated < 0 ? "Overall shortfall" : "Unallocated"}
                </p>
                <strong
                  className={totalUnallocated < 0 ? "text-destructive" : ""}
                >
                  {formatMoney(Math.abs(totalUnallocated), settings)}
                </strong>
              </div>
            </Card>
            <p className="account-explanation">
              Balances use recorded income, expenses and transfers. Allocations
              reserve the full target of each linked goal; they do not move
              money. Opening bank balances are not included.
            </p>
            <section
              className="account-grid"
              aria-label="Accounts and goal allocations"
            >
              {accounts.map((account) => {
                const linkedGoals = goals.filter(
                  (goal) => goal.account_id === account.id,
                );
                const shortfall = account.unallocated_amount < 0;
                const utilisation =
                  account.balance > 0
                    ? Math.min(
                        100,
                        (account.allocated_amount / account.balance) * 100,
                      )
                    : account.allocated_amount > 0
                      ? 100
                      : 0;
                return (
                  <Card className="account-card" key={account.id}>
                    <div className="account-card-head">
                      <div>
                        <p className="metric-label">Account</p>
                        <h2>{account.name}</h2>
                      </div>
                      <div className="record-actions">
                        <button
                          aria-label={`Edit ${account.name}`}
                          onClick={() => setEditing(account)}
                        >
                          <Icon name="edit" />
                        </button>
                        <button
                          aria-label={`Delete ${account.name}`}
                          onClick={() => setDeleting(account)}
                        >
                          <Icon name="trash" />
                        </button>
                      </div>
                    </div>
                    <p className="account-balance">
                      {formatMoney(account.balance, settings)}
                    </p>
                    <p className="account-balance-label">Recorded balance</p>
                    <Progress value={utilisation} />
                    <div className="account-allocation-stats">
                      <div>
                        <p className="metric-label">Allocated to goals</p>
                        <strong>
                          {formatMoney(account.allocated_amount, settings)}
                        </strong>
                      </div>
                      <div>
                        <p className="metric-label">
                          {shortfall ? "Shortfall" : "Unallocated"}
                        </p>
                        <strong
                          className={
                            shortfall ? "text-destructive" : "positive"
                          }
                        >
                          {formatMoney(
                            Math.abs(account.unallocated_amount),
                            settings,
                          )}
                        </strong>
                      </div>
                    </div>
                    <p
                      className={`account-allocation-note ${shortfall ? "text-destructive" : ""}`}
                    >
                      {shortfall
                        ? account.goal_count === 0
                          ? `This account’s recorded balance is ${formatMoney(-account.balance, settings)} below zero.`
                          : `${formatMoney(-account.unallocated_amount, settings)} more is needed to cover this account’s linked goal targets.`
                        : `${formatMoney(account.unallocated_amount, settings)} is not allocated to a goal.`}
                    </p>
                    <div className="account-goals-head">
                      <h3>
                        {account.goal_count} linked{" "}
                        {account.goal_count === 1 ? "goal" : "goals"}
                      </h3>
                      <Button
                        variant="ghost"
                        onClick={() => setGoalAccountId(account.id)}
                      >
                        <Icon name="plus" />
                        Add goal
                      </Button>
                    </div>
                    {linkedGoals.length === 0 ? (
                      <p className="account-no-goals">
                        Link a goal here or choose this account when editing a
                        goal.
                      </p>
                    ) : (
                      <ul className="account-goals-list">
                        {linkedGoals.map((goal) => (
                          <li key={goal.id}>
                            <button
                              onClick={() => setEditingGoal(goal)}
                              aria-label={`Edit goal ${goal.name}`}
                            >
                              <span>{goal.name}</span>
                              <strong>
                                {formatMoney(goal.target_amount, settings)}
                              </strong>
                              <Icon name="edit" />
                            </button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </Card>
                );
              })}
            </section>
          </>
        ))}
    </div>
  );
}
