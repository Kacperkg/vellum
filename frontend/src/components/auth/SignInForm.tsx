import Button from "../Button"
import Input from "../Input"

type SignInFormProps = {
  onRegister: () => void
}

export default function SignInForm({ onRegister }: SignInFormProps) {
  return (
    <div className="flex flex-col">
      <div className="flex flex-col gap-4">
        <Input variant="email" placeholder="john@doe.com" />
        <Input variant="password" placeholder="Password" />
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
      <Button size="lg">Sign in</Button>
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
    </div>
  )
}
