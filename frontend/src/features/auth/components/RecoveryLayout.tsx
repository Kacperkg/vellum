import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import Logo from "@/components/shared/Logo";

type Props = { title: string; description: string; children: ReactNode };

export default function RecoveryLayout({
  title,
  description,
  children,
}: Props) {
  return (
    <main className="flex min-h-screen items-center justify-center px-6 py-12">
      <div className="w-full max-w-md">
        <Logo />
        <div className="flex flex-col gap-3 py-8">
          <h1 className="text-3xl tracking-wide">{title}</h1>
          <p className="text-muted-foreground">{description}</p>
        </div>
        {children}
        <Link
          to="/login"
          className="mt-6 inline-block rounded-sm text-sm underline underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          Back to sign in
        </Link>
      </div>
    </main>
  );
}
