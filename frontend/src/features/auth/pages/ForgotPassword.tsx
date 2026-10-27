import { useId, useState, type SubmitEvent } from "react";
import axios from "axios";
import Button from "@/components/ui/Button";
import Input from "@/components/ui/Input";
import { forgotPassword } from "@/api/endpoints/auth";
import RecoveryLayout from "../components/RecoveryLayout";

export default function ForgotPassword() {
  const id = useId();
  const [email, setEmail] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isSubmitting) return;
    setError("");
    setIsSubmitting(true);
    try {
      await forgotPassword(email);
      setComplete(true);
    } catch (error) {
      setError(
        axios.isAxiosError(error) && error.response?.status === 429
          ? "Too many requests. Please wait a few minutes before trying again."
          : "Unable to request a reset right now. Please try again later.",
      );
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <RecoveryLayout
      title="Forgot your password?"
      description="Enter the email address you used to create your account."
    >
      {complete ? (
        <p role="status">
          If an account exists with this email address, password reset
          instructions will be sent. Please check your inbox and spam folder.
        </p>
      ) : (
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-4"
          aria-busy={isSubmitting}
        >
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <label htmlFor={`${id}-email`}>Email</label>
          <Input
            id={`${id}-email`}
            variant="email"
            autoComplete="email"
            required
            maxLength={254}
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
          <Button type="submit" size="lg" disabled={isSubmitting}>
            {isSubmitting ? "Sending instructions…" : "Send reset instructions"}
          </Button>
        </form>
      )}
    </RecoveryLayout>
  );
}
