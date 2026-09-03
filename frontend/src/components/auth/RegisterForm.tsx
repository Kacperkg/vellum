import Button from "../Button"
import Input from "../Input"

type RegisterFormProps = {
  onSignIn: () => void
}

export default function RegisterForm({ onSignIn }: RegisterFormProps) {
  return (
    <div className="flex flex-col">
      <div className="flex flex-col gap-4 pb-4">
        <Input variant="string" placeholder="First Name" className="bg-surface" />
        <Input variant="string" placeholder="Last Name" className="bg-surface" />
        <Input variant="email" placeholder="Email" />
        <Input variant="password" placeholder="Password" />
      </div>
      <Button size="lg">Create account</Button>
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
    </div>
  )
}
