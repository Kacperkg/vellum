import { useState } from "react";
import Logo from "@/components/shared/Logo";
import RegisterForm from "../components/RegisterForm";
import SignInForm from "../components/SignInForm";

export default function Login() {
  const [register, setRegister] = useState(false);

  return (
    <section className="flex min-h-screen items-center justify-center">
      <div>
        <Logo />
        <div className="flex flex-col gap-3 py-8">
          <h3 className="text-md uppercase tracking-widest text-muted-foreground">
            {register ? "Create account" : "Sign in"}
          </h3>
          <h1 className="text-4xl tracking-wide">
            {register ? "Get started with Vellum." : "Welcome back."}
          </h1>
          <h2 className="text-muted-foreground">
            {register
              ? "A quiet, premium home for your finances."
              : "Sign in to your calm, private budgeting workspace."}
          </h2>
        </div>

        {register ? (
          <RegisterForm onSignIn={() => setRegister(false)} />
        ) : (
          <SignInForm onRegister={() => setRegister(true)} />
        )}
      </div>
    </section>
  );
}
