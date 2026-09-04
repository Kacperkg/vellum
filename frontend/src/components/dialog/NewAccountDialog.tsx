import Input from "../Input";
import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function NewAccountDialog({
  dialogOpen,
  setDialogOpen,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title="New Account"
      description="Give this account a name so you can attach transcation to it."
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>Add account</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h1>Name</h1>
          <Input variant="string" placeholder="Monzo Saving" />
        </div>
      </div>
    </Dialog>
  );
}
