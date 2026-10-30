import { motion, useReducedMotion } from 'motion/react';
import { type ButtonHTMLAttributes, createContext, type HTMLAttributes, useContext, useId } from 'react';
import { Button } from './ui';
import './SelectionGroup.css';

const SelectionContext = createContext('');

export function SelectionGroup({
  as: Tag = 'div',
  className = '',
  children,
  ...props
}: HTMLAttributes<HTMLElement> & { as?: 'div' | 'nav' }) {
  const id = useId();
  return (
    <SelectionContext.Provider value={id}>
      <Tag className={`d-selection-group ${className}`} {...props}>
        {children}
      </Tag>
    </SelectionContext.Provider>
  );
}

export function SelectionButton({
  selected,
  className = '',
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { selected: boolean }) {
  const group = useContext(SelectionContext);
  const reduced = useReducedMotion();
  if (!group) throw new Error('SelectionButton requires a SelectionGroup');
  return (
    <Button className={`d-selection-button ${className}`} {...props}>
      {selected && (
        <motion.span
          className="d-selection-highlight"
          aria-hidden="true"
          initial={false}
          layoutId={reduced ? undefined : group}
          transition={{ type: 'spring', stiffness: 420, damping: 38 }}
        />
      )}
      <span className="d-selection-content">{children}</span>
    </Button>
  );
}
