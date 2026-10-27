import Button from "@/components/ui/Button";
import Input from "@/components/ui/Input";
import { useId, useState, type SubmitEvent } from "react";
import { login } from "@/api/endpoints/auth";
import axios from "axios";
import type { ApiErrorResponse } from "@/api/types";
import { Link, useNavigate } from "@tanstack/react-router";

type SignInFormProps = {
  onRegister: () => void;
};

export default function SignInForm({ onRegister }: SignInFormProps) {
  const formId = useId();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");

  const navigate = useNavigate();

  async function handleSubmit(
    event: SubmitEvent<HTMLFormElement>,
  ): Promise<void> {
    event.preventDefault();
    setErrorMessage("");
    setIsSubmitting(true);
    try {
      await login({
        email,
        password,
      });
      await navigate({
        to: "/",
        replace: true,
      });
    } catch (error) {
      if (axios.isAxiosError<ApiErrorResponse>(error)) {
        setErrorMessage(error.response?.data?.error ?? "Unable to sign in");
      } else {
        setErrorMessage("An unexpected error occurred");
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <form className="flex flex-col" onSubmit={handleSubmit}>
      <div className="flex flex-col gap-4">
        {errorMessage && (
          <p role="alert" className="pb-2 text-sm max-w-2xs text-destructive">
            {errorMessage}
          </p>
        )}
        <label className="sr-only" htmlFor={`${formId}-email`}>
          Email
        </label>
        <Input
          id={`${formId}-email`}
          autoComplete="email"
          required
          variant="email"
          placeholder="john@doe.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <label className="sr-only" htmlFor={`${formId}-password`}>
          Password
        </label>
        <Input
          id={`${formId}-password`}
          autoComplete="current-password"
          required
          variant="password"
          placeholder="Password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </div>
      <div className="flex w-full justify-end py-2">
        <Link
          to="/forgot-password"
          className="rounded-sm py-2 text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          Forgot password?
        </Link>
      </div>
      <Button size="lg" type="submit" disabled={isSubmitting}>
        {isSubmitting ? "Signing in..." : "Sign in"}
      </Button>
      <p className="pt-4 text-sm text-muted-foreground">
        Don't have an account?{" "}
        <Button
          variant="ghost"
          className="cursor-pointer pl-0 hover:bg-transparent!"
          size="sm"
          onClick={onRegister}
        >
          Create one
        </Button>
      </p>
    </form>
  );
}
