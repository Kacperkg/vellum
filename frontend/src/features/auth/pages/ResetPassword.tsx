import { useEffect, useId, useState, type SubmitEvent } from "react";
import { Link } from "@tanstack/react-router";
import axios from "axios";
import Button from "@/components/ui/Button";
import Input from "@/components/ui/Input";
import { resetPassword } from "@/api/endpoints/auth";
import {
  clearResetToken,
  getResetToken,
  passwordError,
} from "../utils/resetToken";
import RecoveryLayout from "../components/RecoveryLayout";

export default function ResetPassword() {
  const id = useId();
  const [token, setToken] = useState(getResetToken);
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [complete, setComplete] = useState(false);
  const [invalid, setInvalid] = useState(!token);
  const [error, setError] = useState("");
  useEffect(clearResetToken, []);

  async function handleSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isSubmitting || !token) return;
    const validation = passwordError(password, confirmation);
    if (validation) {
      setError(validation);
      return;
    }
    setError("");
    setIsSubmitting(true);
    try {
      await resetPassword(token, password);
      setComplete(true);
      setToken(null);
      setPassword("");
      setConfirmation("");
    } catch (error) {
      if (
        axios.isAxiosError(error) &&
        [400, 401, 404, 410].includes(error.response?.status ?? 0)
      ) {
        setInvalid(true);
        setToken(null);
      } else {
        setError(
          axios.isAxiosError(error) && error.response?.status === 429
            ? "Too many attempts. Please wait a few minutes before trying again."
            : "Unable to reset your password right now. Please try again later.",
        );
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <RecoveryLayout
      title="Reset your password"
      description="Choose a new password for your Vellum account."
    >
      {complete ? (
        <p role="status">
          Your password has been reset. Sign in with your new password. All
          previous sessions have been signed out.
        </p>
      ) : invalid ? (
        <div>
          <p role="alert">
            This reset link is invalid or has expired. Request a new link to
            continue.
          </p>
          <Link
            to="/forgot-password"
            className="mt-4 inline-block underline underline-offset-4"
          >
            Request a new reset link
          </Link>
        </div>
      ) : (
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-4"
          aria-busy={isSubmitting}
        >
          <p
            id={`${id}-requirements`}
            className="text-sm text-muted-foreground"
          >
            Use at least 8 characters and at most 72 UTF-8 bytes. Longer
            passwords are encouraged.
          </p>
          {error && (
            <p
              id={`${id}-error`}
              role="alert"
              className="text-sm text-destructive"
            >
              {error}
            </p>
          )}
          <label htmlFor={`${id}-password`}>New password</label>
          <Input
            id={`${id}-password`}
            variant="password"
            autoComplete="new-password"
            required
            minLength={8}
            aria-describedby={`${id}-requirements`}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          <label htmlFor={`${id}-confirmation`}>Confirm new password</label>
          <Input
            id={`${id}-confirmation`}
            variant="password"
            autoComplete="new-password"
            required
            minLength={8}
            aria-describedby={error ? `${id}-error` : undefined}
            aria-invalid={error ? true : undefined}
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
          />
          <Button type="submit" size="lg" disabled={isSubmitting}>
            {isSubmitting ? "Resetting password…" : "Reset password"}
          </Button>
        </form>
      )}
    </RecoveryLayout>
  );
}
