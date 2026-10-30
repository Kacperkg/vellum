import { useId, useState, type FormEvent, type ReactNode } from "react";
import { createAccount } from "@/api/endpoints/accounts";
import { logout } from "@/api/endpoints/auth";
import { apiErrorMessage } from "@/api/errors";
import { useFinanceData } from "@/contexts/FinanceDataContext";
import Logo from "@/components/shared/Logo";
import Button from "@/components/ui/Button";
import Input from "@/components/ui/Input";

// Account existence comes from the server on every app load, including refreshes.
// Keep the app shell and route contents unmounted until that check succeeds.
export default function AccountSetupGate({
  children,
}: {
  children: ReactNode;
}) {
  const { accounts, loading, errors, refresh } = useFinanceData();
  const formId = useId();
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [retrying, setRetrying] = useState(false);

  async function signOut() {
    await logout();
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving || !name.trim()) return;
    setSaving(true);
    setError("");
    try {
      await createAccount({ name: name.trim() });
      await refresh();
    } catch (cause) {
      // A timed-out response may still have committed. Recheck before offering
      // another creation attempt, so reload and retry use the same server truth.
      await refresh();
      setError(
        apiErrorMessage(
          cause,
          "The account could not be saved. Please try again.",
        ),
      );
    } finally {
      setSaving(false);
    }
  }

  if (!loading && !errors.accounts && accounts.length > 0) return children;

  return (
    <main className="flex min-h-screen items-center justify-center p-6">
      <section className="w-full max-w-md" aria-label="Account setup">
        <Logo />
        {loading ? (
          <p role="status" className="py-8">
            Checking your accounts…
          </p>
        ) : errors.accounts ? (
          <div className="flex flex-col gap-4 py-8">
            <h1 className="text-3xl">Unable to check your accounts</h1>
            <p role="alert">
              We could not confirm your account setup. Please try again.
            </p>
            <Button
              disabled={retrying}
              onClick={async () => {
                setRetrying(true);
                try {
                  await refresh();
                } finally {
                  setRetrying(false);
                }
              }}
            >
              {retrying ? "Checking…" : "Try again"}
            </Button>
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-3 py-8">
              <p className="text-sm uppercase tracking-widest text-muted-foreground">
                Finish setting up Vellum
              </p>
              <h1 className="text-3xl">Add your first bank account</h1>
              <p className="text-muted-foreground">
                Give your account a name to start tracking your finances. You
                can add more accounts later.
              </p>
              <p className="text-sm text-muted-foreground">
                This creates a record in Vellum. No bank login or connection is
                needed.
              </p>
            </div>
            <form className="flex flex-col gap-4" onSubmit={submit}>
              <label htmlFor={`${formId}-name`}>Account name</label>
              <Input
                id={`${formId}-name`}
                required
                maxLength={100}
                placeholder="Everyday account"
                value={name}
                onChange={(event) => setName(event.target.value)}
                disabled={saving}
              />
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <Button type="submit" disabled={saving || !name.trim()}>
                {saving ? "Saving…" : "Add account and continue"}
              </Button>
            </form>
          </>
        )}
        <Button
          variant="ghost"
          className="mt-4"
          onClick={() => void signOut()}
          disabled={saving}
        >
          Sign out
        </Button>
      </section>
    </main>
  );
}
