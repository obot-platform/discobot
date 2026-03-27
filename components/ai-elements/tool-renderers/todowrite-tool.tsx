import { CheckCircle, Circle, Clock, ListTodo } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  ToolInput as DefaultToolInput,
  ToolOutput as DefaultToolOutput,
} from "../tool";
import type { ToolRendererProps } from "../tool-schemas";
import {
  type TodoWriteToolInput,
  type TodoWriteToolOutput,
  validateTodoWriteInput,
  validateTodoWriteOutput,
} from "../tool-schemas/todowrite-schema";

/**
 * TodoWriteToolRenderer - Optimized renderer for TodoWrite tool
 *
 * Displays todo list operations with:
 * - Todo count badge
 * - Status indicators (pending/in_progress/completed)
 * - Visual checkboxes
 * - Color-coded status icons
 */
export default function TodoWriteToolRenderer({
  input,
  output,
  errorText,
  state,
}: ToolRendererProps<TodoWriteToolInput, TodoWriteToolOutput>) {
  // Check if streaming
  const isStreaming =
    state === "input-streaming" || state === "input-available";

  // During streaming, input may be undefined or incomplete - handle gracefully
  if (!input || typeof input !== "object") {
    return (
      <div className="p-4 text-muted-foreground text-sm">
        {isStreaming ? "Loading..." : "No input data"}
      </div>
    );
  }

  // Validate input
  const inputValidation = validateTodoWriteInput(input);

  if (!inputValidation.success) {
    // During streaming, validation may fail due to incomplete input - don't log spam
    if (!isStreaming) {
      console.warn(
        `TodoWrite tool input validation failed: ${inputValidation.error}`,
      );
    }

    // Show loading state during streaming, fallback to generic display otherwise
    if (isStreaming) {
      return (
        <div className="p-4 text-muted-foreground text-sm">
          Loading todo list...
        </div>
      );
    }

    return (
      <>
        <DefaultToolInput input={input} />
        <DefaultToolOutput output={output} errorText={errorText} />
      </>
    );
  }

  // biome-ignore lint/style/noNonNullAssertion: Validated above
  const validInput = inputValidation.data!;

  // Check if todos array exists
  if (!validInput.todos || validInput.todos.length === 0) {
    return (
      <div className="p-4 text-muted-foreground text-sm">
        {isStreaming ? "Loading todo list..." : "No todos provided"}
      </div>
    );
  }

  // Validate output if present
  const outputValidation = output ? validateTodoWriteOutput(output) : null;
  const validOutput = (
    outputValidation?.success ? outputValidation.data : null
  ) as TodoWriteToolOutput | null;
  const hasSuccessState = Boolean(validOutput?.success || validOutput?.content);

  // Count todos by status
  const statusCounts = {
    pending: validInput.todos.filter((t) => t.status === "pending").length,
    in_progress: validInput.todos.filter((t) => t.status === "in_progress")
      .length,
    completed: validInput.todos.filter((t) => t.status === "completed").length,
  };

  return (
    <div className="space-y-4 p-4">
      {/* Header Section */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <ListTodo className="size-4 text-muted-foreground" />
          <h4 className="font-medium text-muted-foreground text-xs uppercase tracking-wide">
            Task List
          </h4>
          <span className="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs">
            {validInput.todos.length}{" "}
            {validInput.todos.length === 1 ? "task" : "tasks"}
          </span>
        </div>

        {/* Status summary */}
        <div className="flex flex-wrap gap-2 text-xs">
          {statusCounts.completed > 0 && (
            <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
              <CheckCircle className="size-3" />
              {statusCounts.completed} completed
            </span>
          )}
          {statusCounts.in_progress > 0 && (
            <span className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
              <Clock className="size-3" />
              {statusCounts.in_progress} in progress
            </span>
          )}
          {statusCounts.pending > 0 && (
            <span className="flex items-center gap-1 text-muted-foreground">
              <Circle className="size-3" />
              {statusCounts.pending} pending
            </span>
          )}
        </div>
      </div>

      {/* Todos List */}
      <div className="space-y-2">
        {validInput.todos.map(
          (
            todo: {
              content?: string;
              status?: string;
              activeForm?: string;
            },
            _idx: number,
          ) => {
            // Provide defaults for optional fields
            const content = todo.content || "";
            const status = todo.status || "pending";
            const activeForm = todo.activeForm || "";

            return (
              <div
                key={`${content}-${status}`}
                className={cn(
                  "flex items-start gap-3 rounded-md border p-3 transition-colors",
                  status === "completed"
                    ? "border-green-200 bg-green-50 dark:border-green-900 dark:bg-green-950/20"
                    : status === "in_progress"
                      ? "border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950/20"
                      : "border-border bg-muted/30",
                )}
              >
                {/* Status Icon */}
                <div className="shrink-0 pt-0.5">
                  {status === "completed" ? (
                    <CheckCircle className="size-4 text-green-600 dark:text-green-400" />
                  ) : status === "in_progress" ? (
                    <Clock className="size-4 text-blue-600 dark:text-blue-400" />
                  ) : (
                    <Circle className="size-4 text-muted-foreground" />
                  )}
                </div>

                {/* Todo Content */}
                <div className="flex-1">
                  <p
                    className={cn(
                      "text-foreground text-sm",
                      status === "completed" && "line-through",
                    )}
                  >
                    {content}
                  </p>
                  {activeForm &&
                    activeForm !== content &&
                    status === "in_progress" && (
                      <p className="mt-1 italic text-muted-foreground text-xs">
                        {activeForm}
                      </p>
                    )}
                </div>

                {/* Status Badge */}
                <span
                  className={cn(
                    "shrink-0 rounded-full px-2 py-0.5 text-xs",
                    status === "completed"
                      ? "bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400"
                      : status === "in_progress"
                        ? "bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400"
                        : "bg-muted text-muted-foreground",
                  )}
                >
                  {status.replace("_", " ")}
                </span>
              </div>
            );
          },
        )}
      </div>

      {/* Error or Success Section */}
      {(errorText || validOutput?.error) && (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm">
          {errorText || validOutput?.error}
        </div>
      )}

      {hasSuccessState && (
        <div className="flex items-center gap-2 text-green-600 text-sm dark:text-green-400">
          <CheckCircle className="size-4" />
          <span>Task list updated successfully</span>
        </div>
      )}
    </div>
  );
}
