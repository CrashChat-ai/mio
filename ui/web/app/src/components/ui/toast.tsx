import * as React from "react";
import * as ToastPrimitives from "@radix-ui/react-toast";
import { cva, type VariantProps } from "class-variance-authority";
import { X } from "lucide-react";
import { cn } from "../../lib/cn";

const ToastProvider = ToastPrimitives.Provider;

function ToastViewport({ className, ...props }: React.ComponentProps<typeof ToastPrimitives.Viewport>) {
  return (
    <ToastPrimitives.Viewport
      className={cn(
        "fixed bottom-0 right-0 z-[100] flex max-h-screen w-full flex-col gap-2 p-4 sm:max-w-[380px]",
        className,
      )}
      {...props}
    />
  );
}

const toastVariants = cva(
  "group pointer-events-auto relative flex w-full items-center justify-between gap-3 overflow-hidden rounded-lg border p-4 pr-8 shadow-elev-raised transition-all data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[var(--radix-toast-swipe-end-x)] data-[swipe=move]:translate-x-[var(--radix-toast-swipe-move-x)] data-[swipe=move]:transition-none data-[state=closed]:opacity-0",
  {
    variants: {
      variant: {
        default: "border-border bg-surface text-fg",
        error: "border-danger/40 bg-surface text-danger",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

type ToastProps = React.ComponentProps<typeof ToastPrimitives.Root> &
  VariantProps<typeof toastVariants>;

function Toast({ className, variant, ...props }: ToastProps) {
  return <ToastPrimitives.Root className={cn(toastVariants({ variant }), className)} {...props} />;
}

function ToastClose({ className, ...props }: React.ComponentProps<typeof ToastPrimitives.Close>) {
  return (
    <ToastPrimitives.Close
      className={cn(
        "absolute right-2 top-2 rounded-sm p-1 text-muted opacity-0 transition-opacity hover:text-fg focus-visible:opacity-100 focus-visible:outline-none group-hover:opacity-100",
        className,
      )}
      toast-close=""
      {...props}
    >
      <X size={14} />
    </ToastPrimitives.Close>
  );
}

function ToastTitle({ className, ...props }: React.ComponentProps<typeof ToastPrimitives.Title>) {
  return (
    <ToastPrimitives.Title className={cn("text-sm font-semibold", className)} {...props} />
  );
}

function ToastDescription({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitives.Description>) {
  return (
    <ToastPrimitives.Description
      className={cn("text-sm text-fg-2", className)}
      {...props}
    />
  );
}

type ToastActionElement = React.ReactElement;

export {
  ToastProvider,
  ToastViewport,
  Toast,
  ToastTitle,
  ToastDescription,
  ToastClose,
  type ToastProps,
  type ToastActionElement,
};
