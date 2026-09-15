import Button from "../Button";
import Input from "../Input";
import { useState, type SubmitEvent } from "react";
import { register } from "../../api/endpoints/auth";
import axios from "axios";
import type { ApiErrorResponse } from "../../api/types";
import { useNavigate } from "@tanstack/react-router";

type RegisterFormProps = {
  onSignIn: () => void;
};

export default function RegisterForm({ onSignIn }: RegisterFormProps) {
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
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
      await register({
        first_name: firstName,
        last_name: lastName,
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
      <div className="flex flex-col gap-4 pb-4">
        {errorMessage && (
          <p role="alert" className="pb-2 text-sm max-w-2xs text-destructive">
            {errorMessage}
          </p>
        )}
        <Input
          required
          variant="string"
          placeholder="First Name"
          className="bg-surface"
          value={firstName}
          onChange={(event) => setFirstName(event.target.value)}
        />
        <Input
          required
          variant="string"
          placeholder="Last Name"
          className="bg-surface"
          value={lastName}
          onChange={(event) => setLastName(event.target.value)}
        />
        <Input
          required
          variant="email"
          placeholder="Email"
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
      <Button size="lg" type="submit" disabled={isSubmitting}>
        Create account
      </Button>
      <p className="pt-4 text-sm text-muted-foreground">
        Already have an account?{" "}
        <Button
          variant="ghost"
          className="cursor-pointer pl-0 hover:bg-transparent!"
          size="sm"
          onClick={onSignIn}
        >
          Sign in
        </Button>
      </p>
    </form>
  );
}
