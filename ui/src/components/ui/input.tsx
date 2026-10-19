import { forwardRef } from "react";
import * as LabelPrimitive from "@radix-ui/react-label";
import { cn } from "@/lib/utils";
import { controlBase, controlSize, type Size } from "./variants";

export interface FieldProps {
  label?: string;
  hint?: string;
  error?: string;
  required?: boolean;
  htmlFor?: string;
  className?: string;
  children: React.ReactNode;
}

/** Label + control + hint/error wrapper. Keeps accessible labelling consistent. */
export const Field = ({ label, hint, error, required, htmlFor, className, children }: FieldProps) => (
  <div className={cn("space-y-1.5", className)}>
    {label ? (
      <LabelPrimitive.Root htmlFor={htmlFor} className="block text-body-3 font-medium text-ink">
        {label}
        {required ? <span className="ml-0.5 text-crit">*</span> : null}
      </LabelPrimitive.Root>
    ) : null}
    {children}
    {error ? (
      <p role="alert" className="text-body-3 text-crit">
        {error}
      </p>
    ) : hint ? (
      <p className="text-body-3 text-ink-faint">{hint}</p>
    ) : null}
  </div>
);

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  inputSize?: Size;
  invalid?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ inputSize = "md", invalid, className, ...rest }, ref) => (
    <input
      ref={ref}
      className={cn(controlBase, controlSize[inputSize], invalid && "border-crit focus:border-crit focus:ring-crit/20", className)}
      {...rest}
    />
  ),
);
Input.displayName = "Input";

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ invalid, className, rows = 4, ...rest }, ref) => (
    <textarea
      ref={ref}
      rows={rows}
      className={cn(controlBase, "resize-y py-2 px-3 leading-relaxed", invalid && "border-crit focus:border-crit focus:ring-crit/20", className)}
      {...rest}
    />
  ),
);
Textarea.displayName = "Textarea";
