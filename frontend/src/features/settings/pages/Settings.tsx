import { useId, useState } from "react";
import { Link } from "@tanstack/react-router";
import { deleteAccount } from "@/api/endpoints/accounts";
import { updateSettings } from "@/api/endpoints/settings";
import type { UpdateUserSettingsRequest } from "@/api/types";
import Button from "@/components/ui/Button";
import Card from "@/components/ui/Card";
import DataState from "@/components/shared/DataState";
import Icon from "@/components/ui/Icon";
import PageHeader from "@/components/shared/PageHeader";
import Select from "@/components/ui/Select";
import { NewAccountDialog } from "@/features/accounts";
import DeleteDialog from "@/components/shared/DeleteDialog";
import type { Account } from "@/api/types";
import { apiErrorMessage } from "@/api/errors";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import { NewTransferDialog } from "@/features/transactions";
import { formatDate, formatMoney } from "@/utils/finance";

const defaults: UpdateUserSettingsRequest = {
  currency: "GBP",
  date_format: "DD/MM/YYYY",
  number_format: "comma_decimal",
  week_starts_on: "Monday",
};

export default function Settings() {
  const fieldId = useId();
  const { accounts, settings, loading, errors, refresh } = useFinanceData();
  const error = errors.accounts ?? errors.settings;
  const [accountOpen, setAccountOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [mutationError, setMutationError] = useState("");
  const [saved, setSaved] = useState(false);
  const [editing, setEditing] = useState<Account | null>(null);
  const [deleting, setDeleting] = useState<Account | null>(null);

  async function save(next: UpdateUserSettingsRequest) {
    if (saving) return;
    setSaving(true);
    setMutationError("");
    setSaved(false);
    try {
      await updateSettings(next);
      await refresh();
      setSaved(true);
    } catch (error) {
      setMutationError(
        apiErrorMessage(
          error,
          "Your preferences could not be saved. Please try again.",
        ),
      );
    } finally {
      setSaving(false);
    }
  }

  const currencyPreview = settings ? formatMoney(123456, settings) : "";
  const datePreview = settings
    ? formatDate("2026-09-03T12:00:00Z", settings.date_format)
    : "";
  const numberPreview =
    settings?.number_format === "dot_decimal"
      ? "12.345,67"
      : settings?.number_format === "apostrophe"
        ? "12'345.67"
        : "12,345.67";

  return (
    <div className="page-shell settings-page">
      <PageHeader
        eyebrow="Workspace"
        title="Settings"
        description="Your accounts and preferences, all in one place."
      />
      {accountOpen && (
        <NewAccountDialog
          dialogOpen={accountOpen}
          setDialogOpen={setAccountOpen}
        />
      )}
      {transferOpen && (
        <NewTransferDialog
          dialogOpen={transferOpen}
          setDialogOpen={setTransferOpen}
        />
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
      {mutationError && (
        <p role="alert" className="mutation-error">
          {mutationError}
        </p>
      )}
      <DataState
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
      />
      {!loading && !error && settings && (
        <div className="settings-stack">
          <section
            className="settings-section"
            aria-labelledby={`${fieldId}-accounts-heading`}
          >
            <div className="settings-section-intro">
              <span className="settings-section-icon">
                <Icon name="accounts" />
              </span>
              <h2 id={`${fieldId}-accounts-heading`}>Accounts</h2>
              <p>
                Manage where your money lives and keep your accounts in order.
              </p>
            </div>
            <Card className="settings-card account-settings-card">
              <div className="settings-card-title">
                <div>
                  <div className="account-settings-heading">
                    <h3>Your accounts</h3>
                    <span>{accounts.length}</span>
                  </div>
                </div>
                <div className="account-settings-actions">
                  <Button
                    variant="outline"
                    disabled={accounts.length < 2}
                    title={
                      accounts.length < 2
                        ? "Add a second account to make transfers"
                        : "Transfer between accounts"
                    }
                    onClick={() => setTransferOpen(true)}
                  >
                    <Icon name="transactions" />
                    Transfer
                  </Button>
                  <Button onClick={() => setAccountOpen(true)}>
                    <Icon name="plus" />
                    Add account
                  </Button>
                </div>
              </div>
              {accounts.length === 0 ? (
                <div className="settings-accounts-empty">
                  <span className="settings-account-icon">
                    <Icon name="accounts" />
                  </span>
                  <div>
                    <strong>No accounts yet</strong>
                    <p>
                      Add your first account to start organising your money.
                    </p>
                  </div>
                </div>
              ) : (
                <ul
                  className="settings-account-list"
                  aria-label="Your accounts"
                >
                  {accounts.map((account) => (
                    <li className="settings-account-row" key={account.id}>
                      <span className="settings-account-icon">
                        <Icon name="accounts" />
                      </span>
                      <div className="settings-account-identity">
                        <strong>{account.name}</strong>
                        <span>
                          {account.goal_count === 0
                            ? "No linked goals"
                            : `${account.goal_count} linked ${account.goal_count === 1 ? "goal" : "goals"}`}
                        </span>
                      </div>
                      <div className="settings-account-balance">
                        <strong>
                          {formatMoney(account.balance, settings)}
                        </strong>
                        <span>Recorded balance</span>
                      </div>
                      <div className="record-actions settings-account-actions">
                        <button
                          onClick={() => setEditing(account)}
                          aria-label={`Edit ${account.name}`}
                          title={`Edit ${account.name}`}
                        >
                          <Icon name="edit" />
                        </button>
                        <button
                          onClick={() => setDeleting(account)}
                          aria-label={`Delete ${account.name}`}
                          title={`Delete ${account.name}`}
                        >
                          <Icon name="trash" />
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              <div className="account-settings-footer">
                <p>Balances reflect recorded transactions and transfers.</p>
                <Link to="/accounts">
                  View accounts <Icon name="arrow" />
                </Link>
              </div>
            </Card>
          </section>
          <section
            className="settings-section"
            aria-labelledby={`${fieldId}-display-heading`}
          >
            <div className="settings-section-intro">
              <span className="settings-section-icon">
                <Icon name="settings" />
              </span>
              <h2 id={`${fieldId}-display-heading`}>Display & format</h2>
              <p>A few small details to make your numbers feel familiar.</p>
            </div>
            <Card className="preferences-card">
              <div className="preference-row">
                <div className="preference-description">
                  <label htmlFor={`${fieldId}-currency`}>Currency</label>
                  <p id={`${fieldId}-currency-description`}>
                    The currency used for amounts across Vellum.
                  </p>
                </div>
                <div className="preference-control">
                  <Select
                    id={`${fieldId}-currency`}
                    aria-describedby={`${fieldId}-currency-description`}
                    value={settings.currency}
                    disabled={saving}
                    onChange={(event) =>
                      void save({
                        ...settings,
                        currency: event.target
                          .value as UpdateUserSettingsRequest["currency"],
                      })
                    }
                  >
                    <option value="USD">US Dollar (USD)</option>
                    <option value="GBP">British Pound (GBP)</option>
                    <option value="EUR">Euro (EUR)</option>
                  </Select>
                  <div className="preference-preview">
                    <span>Preview</span>
                    <output htmlFor={`${fieldId}-currency`}>
                      {currencyPreview}
                    </output>
                  </div>
                </div>
              </div>
              <div className="preference-row">
                <div className="preference-description">
                  <label htmlFor={`${fieldId}-date`}>Date format</label>
                  <p id={`${fieldId}-date-description`}>
                    How dates appear on transactions, goals and reports.
                  </p>
                </div>
                <div className="preference-control">
                  <Select
                    id={`${fieldId}-date`}
                    aria-describedby={`${fieldId}-date-description`}
                    value={settings.date_format}
                    disabled={saving}
                    onChange={(event) =>
                      void save({
                        ...settings,
                        date_format: event.target
                          .value as UpdateUserSettingsRequest["date_format"],
                      })
                    }
                  >
                    <option value="MM/DD/YYYY">Month / Day / Year</option>
                    <option value="DD/MM/YYYY">Day / Month / Year</option>
                    <option value="YYYY-MM-DD">Year / Month / Day</option>
                  </Select>
                  <div className="preference-preview">
                    <span>Preview</span>
                    <output htmlFor={`${fieldId}-date`}>{datePreview}</output>
                  </div>
                </div>
              </div>
              <div className="preference-row">
                <div className="preference-description">
                  <label htmlFor={`${fieldId}-number`}>Number format</label>
                  <p id={`${fieldId}-number-description`}>
                    Choose your thousands and decimal separators.
                  </p>
                </div>
                <div className="preference-control">
                  <Select
                    id={`${fieldId}-number`}
                    aria-describedby={`${fieldId}-number-description`}
                    value={settings.number_format}
                    disabled={saving}
                    onChange={(event) =>
                      void save({
                        ...settings,
                        number_format: event.target
                          .value as UpdateUserSettingsRequest["number_format"],
                      })
                    }
                  >
                    <option value="comma_decimal">12,345.67</option>
                    <option value="dot_decimal">12.345,67</option>
                    <option value="apostrophe">12'345.67</option>
                  </Select>
                  <div className="preference-preview">
                    <span>Preview</span>
                    <output htmlFor={`${fieldId}-number`}>
                      {numberPreview}
                    </output>
                  </div>
                </div>
              </div>
              <div className="preference-row">
                <div className="preference-description">
                  <label htmlFor={`${fieldId}-week`}>Week starts on</label>
                  <p id={`${fieldId}-week-description`}>
                    Set the first day for weekly views and calendars.
                  </p>
                </div>
                <div className="preference-control">
                  <Select
                    id={`${fieldId}-week`}
                    aria-describedby={`${fieldId}-week-description`}
                    value={settings.week_starts_on}
                    disabled={saving}
                    onChange={(event) =>
                      void save({
                        ...settings,
                        week_starts_on: event.target
                          .value as UpdateUserSettingsRequest["week_starts_on"],
                      })
                    }
                  >
                    <option>Monday</option>
                    <option>Sunday</option>
                  </Select>
                  <div className="preference-preview">
                    <span>Your week</span>
                    <output htmlFor={`${fieldId}-week`}>
                      {settings.week_starts_on === "Monday"
                        ? "Mon → Sun"
                        : "Sun → Sat"}
                    </output>
                  </div>
                </div>
              </div>
              <div className="preferences-footer">
                <p
                  role="status"
                  className={saved && !saving ? "preferences-saved" : ""}
                >
                  <span aria-hidden="true" />
                  {saving
                    ? "Saving preferences…"
                    : saved
                      ? "Preferences saved"
                      : "Changes save automatically"}
                </p>
                <Button
                  variant="ghost"
                  disabled={saving}
                  onClick={() => void save(defaults)}
                >
                  <Icon name="refresh" />
                  Restore defaults
                </Button>
              </div>
            </Card>
          </section>
        </div>
      )}
    </div>
  );
}
