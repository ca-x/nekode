export type StepperStep = {
  id: string;
  label: string;
};

/**
 * Horizontal stepper indicator for the Add Computer wizard. Renders a
 * numbered pill per step with the active and completed states distinguished
 * by color alone + iconography so colorblind users still see the state.
 */
export function Stepper({ steps, activeIndex }: { steps: readonly StepperStep[]; activeIndex: number }) {
  return (
    <ol className="stepper" aria-label="Progress">
      {steps.map((step, index) => {
        const state = index < activeIndex ? "done" : index === activeIndex ? "active" : "upcoming";
        return (
          <li className={`stepper-step stepper-${state}`} key={step.id} aria-current={state === "active" ? "step" : undefined}>
            <span className="stepper-index tabular-nums" aria-hidden="true">
              {index + 1}
            </span>
            <span className="stepper-label">{step.label}</span>
          </li>
        );
      })}
    </ol>
  );
}
