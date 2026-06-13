import { useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import { useCreateTenant } from "../../queries/tenants";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../../components/ui/dialog";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { toast } from "../../components/ui/use-toast";

export function CreateTenantDialog({ disabled }: { disabled: boolean }) {
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const createTenant = useCreateTenant();

  function submit(event: FormEvent) {
    event.preventDefault();
    createTenant.mutate(
      { slug: slug.trim(), displayName: displayName.trim() },
      {
        onSuccess: (data) => {
          toast({ title: "Tenant created", description: data.tenant.slug });
          setOpen(false);
          setSlug("");
          setDisplayName("");
        },
        onError: (error) => {
          toast({ variant: "error", title: "Create tenant failed", description: error.message });
        },
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant="primary"
          disabled={disabled}
          title={disabled ? "Requires operator role" : undefined}
        >
          <Plus size={14} />
          Create tenant
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create tenant</DialogTitle>
          <DialogDescription>
            The slug is permanent and becomes part of webhook routes.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="tenant-slug">Slug</Label>
            <Input
              id="tenant-slug"
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              placeholder="acme-prod"
              autoComplete="off"
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="tenant-display-name">Display name</Label>
            <Input
              id="tenant-display-name"
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="Acme production"
              autoComplete="off"
            />
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button">Cancel</Button>
            </DialogClose>
            <Button
              type="submit"
              variant="primary"
              disabled={createTenant.isPending || slug.trim() === ""}
            >
              {createTenant.isPending ? "Creating…" : "Create tenant"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
