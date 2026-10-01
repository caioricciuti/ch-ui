// Shared by the forms that email something: once the request went through,
// the fields are replaced by a short panel. The server answers the same
// whatever it decided to send (a new license, an existing one, nothing
// inside a cooldown), so the panel only says what to do next.
export function showDone(form: HTMLFormElement, title: string, text: string): void {
  const fields = form.querySelector<HTMLElement>(".fields");
  if (!fields) return;
  const done = document.createElement("div");
  done.className = "chui-done";
  done.setAttribute("role", "status");
  const strong = document.createElement("strong");
  strong.textContent = title;
  const span = document.createElement("span");
  span.textContent = text;
  done.append(strong, span);
  fields.replaceWith(done);
  form.querySelector<HTMLElement>(".chui-status")?.setAttribute("hidden", "");
}
