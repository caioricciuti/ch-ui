import { cn } from "@/lib/utils";
import { forwardRef } from "react";

const GlassCard = forwardRef<
    HTMLDivElement,
    React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
    <div
        ref={ref}
        className={cn(
            "rounded-xl border border-white/10 bg-black/40 backdrop-blur-xl shadow-2xl ring-1 ring-white/10",
            className
        )}
        {...props}
    />
));
GlassCard.displayName = "GlassCard";

const GlassCardHeader = forwardRef<
    HTMLDivElement,
    React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
    <div
        ref={ref}
        className={cn("flex flex-col space-y-1.5 p-6", className)}
        {...props}
    />
));
GlassCardHeader.displayName = "GlassCardHeader";

const GlassCardTitle = forwardRef<
    HTMLParagraphElement,
    React.HTMLAttributes<HTMLHeadingElement>
>(({ className, ...props }, ref) => (
    <h3
        ref={ref}
        className={cn(
            "text-2xl font-semibold leading-none tracking-tight text-white",
            className
        )}
        {...props}
    />
));
GlassCardTitle.displayName = "GlassCardTitle";

const GlassCardDescription = forwardRef<
    HTMLParagraphElement,
    React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
    <p
        ref={ref}
        className={cn("text-sm text-gray-400", className)}
        {...props}
    />
));
GlassCardDescription.displayName = "GlassCardDescription";

const GlassCardContent = forwardRef<
    HTMLDivElement,
    React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
    <div ref={ref} className={cn("p-6 pt-0", className)} {...props} />
));
GlassCardContent.displayName = "GlassCardContent";

const GlassCardFooter = forwardRef<
    HTMLDivElement,
    React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
    <div
        ref={ref}
        className={cn("flex items-center p-6 pt-0", className)}
        {...props}
    />
));
GlassCardFooter.displayName = "GlassCardFooter";

export {
    GlassCard,
    GlassCardHeader,
    GlassCardFooter,
    GlassCardTitle,
    GlassCardDescription,
    GlassCardContent,
};
