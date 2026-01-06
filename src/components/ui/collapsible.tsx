import * as React from "react"
import { motion, AnimatePresence } from "framer-motion"
import { cn } from "@/lib/utils"

// Context
interface CollapsibleContextValue {
    open: boolean;
    onOpenChange: (open: boolean) => void;
}
const CollapsibleContext = React.createContext<CollapsibleContextValue>({ open: false, onOpenChange: () => { } });

const Collapsible = React.forwardRef<
    HTMLDivElement,
    {
        open?: boolean
        onOpenChange?: (open: boolean) => void
        defaultOpen?: boolean
        children: React.ReactNode
        className?: string
    }
>(({ open, onOpenChange, defaultOpen, children, className, ...props }, ref) => {
    const [uncontrolledOpen, setUncontrolledOpen] = React.useState(defaultOpen || false);
    const isControlled = open !== undefined;
    const currentOpen = isControlled ? open : uncontrolledOpen;
    const handleChange = isControlled ? onOpenChange : setUncontrolledOpen;

    return (
        <CollapsibleContext.Provider value={{ open: !!currentOpen, onOpenChange: handleChange || (() => { }) }}>
            <div ref={ref} className={className} {...props} data-state={currentOpen ? "open" : "closed"}>
                {children}
            </div>
        </CollapsibleContext.Provider>
    )
})
Collapsible.displayName = "Collapsible"

const CollapsibleTrigger = React.forwardRef<
    HTMLButtonElement,
    React.ButtonHTMLAttributes<HTMLButtonElement>
>(({ children, className, onClick, ...props }, ref) => {
    return (
        <CollapsibleContext.Consumer>
            {({ open, onOpenChange }) => (
                <button
                    ref={ref}
                    type="button"
                    onClick={(e) => {
                        onOpenChange?.(!open)
                        onClick?.(e)
                    }}
                    className={cn(className)}
                    {...props}
                >
                    {children}
                </button>
            )}
        </CollapsibleContext.Consumer>
    )
})
CollapsibleTrigger.displayName = "CollapsibleTrigger"

const CollapsibleContent = React.forwardRef<
    HTMLDivElement,
    React.HTMLAttributes<HTMLDivElement>
>(({ children, className, ...props }, ref) => {
    return (
        <CollapsibleContext.Consumer>
            {({ open }) => (
                <AnimatePresence initial={false}>
                    {open && ( // Use conditional rendering for AnimatePresence
                        <motion.div
                            ref={ref}
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: "auto", opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className={cn("overflow-hidden", className)}
                            {...props as any}
                        >
                            {children}
                        </motion.div>
                    )}
                </AnimatePresence>
            )}
        </CollapsibleContext.Consumer>
    )
})
CollapsibleContent.displayName = "CollapsibleContent"

export { Collapsible, CollapsibleTrigger, CollapsibleContent }
