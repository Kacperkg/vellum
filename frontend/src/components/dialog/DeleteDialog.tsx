import Dialog from "./Dialog";
import Button from "../Button";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  type: keyof typeof deleteTypes;
  input: string;
};

const deleteTypes = {
  Transaction: {
    description: (input: string) =>
      `This will remove ${input} from your history. This can't be undone.`,
  },
  Budget: {
    description: (input: string) =>
      `This will remove ${input} budget for this month`,
  },
  Goal: {
    description: (input: string) => `This will remove ${input} goal.`,
  },
  Subscription: {
    description: (input: string) => `This will remove ${input} subscription.`,
  },
};

export default function DeleteDialog({
  dialogOpen,
  setDialogOpen,
  type,
  input,
}: DialogProps) {
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={setDialogOpen}
      title={"Delete " + type}
      description={deleteTypes[type].description(input)}
      headerBorder={false}
      footerBorder={false}
      footer={
        <>
          <Button variant="outline" onClick={() => setDialogOpen(false)}>
            Cancel
          </Button>
          <Button onClick={() => setDialogOpen(false)}>Delete</Button>
        </>
      }
    ></Dialog>
  );
}
