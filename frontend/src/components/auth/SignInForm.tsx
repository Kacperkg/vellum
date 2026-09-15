import Button from "../Button";
import Input from "../Input";
import { useState, type SubmitEvent } from "react";
import { login } from "../../api/endpoints/auth";
import axios from "axios";
import type { ApiErrorResponse } from "../../api/types";
import { useNavigate } from "@tanstack/react-router";

type SignInFormProps = {
  onRegister: () => void;
};

export default function SignInForm({ onRegister }: SignInFormProps) {
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
        <Input
          required
          variant="email"
          placeholder="john@doe.com"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <Input
          required
          variant="password"
          placeholder="Password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </div>
      <div className="flex w-full justify-end py-2">
        <Button
          variant="ghost"
          className="cursor-pointer pr-0 text-muted-foreground hover:bg-transparent!"
          size="sm"
        >
          Forgot password?
        </Button>
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
