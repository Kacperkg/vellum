import { useState } from "react"
import Button from "../components/Button"
import Card, {
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "../components/Card"
import Dialog from "../components/Dialog"
import Input from "../components/Input"
import Select from "../components/Select"

export default function ComponentsDemo() {
  const [dialogOpen, setDialogOpen] = useState(false)

  return (
    <main className="p-8">
      <h1 className="text-2xl font-semibold">Component demos</h1>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Buttons</h2>
        <div className="flex flex-wrap items-center gap-4">
          <Button variant="primary" size="sm">Primary</Button>
          <Button variant="secondary" size="md">Secondary</Button>
          <Button variant="destructive" size="lg">Destructive</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button onClick={() => setDialogOpen(true)}>Open dialog</Button>
        </div>
      </section>

      <Dialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Test dialog"
        description="Use this dialog to check the component's layout and behavior."
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialogOpen(false)}>Cancel</Button>
            <Button onClick={() => setDialogOpen(false)}>Confirm</Button>
          </>
        }
      >
        The dialog component is working.
      </Dialog>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Form controls</h2>
        <div className="flex w-fit flex-col gap-4">
          <Input type="text" placeholder="Text" />
          <Input variant="email" placeholder="Email" />
          <Input variant="password" placeholder="Password" />
          <Input variant="number" placeholder="Number" />
          <Select defaultValue="">
            <option value="" disabled>Select an option</option>
            <option value="one">One</option>
            <option value="two">Two</option>
          </Select>
          <Input variant="date" />
        </div>
      </section>

      <section className="mt-8">
        <h2 className="mb-4 text-lg font-semibold">Card</h2>
        <Card className="w-80">
          <CardHeader>
            <CardTitle>Example card</CardTitle>
            <CardDescription>A quick preview of the card component.</CardDescription>
          </CardHeader>
          <CardContent>Cards can contain any content or controls.</CardContent>
          <CardFooter>
            <Button variant="outline">Cancel</Button>
            <Button>Continue</Button>
          </CardFooter>
        </Card>
      </section>
    </main>
  )
}
