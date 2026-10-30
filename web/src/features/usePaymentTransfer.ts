import type { RefObject } from 'react';
import { useLayoutEffect, useRef } from 'react';

type Props = {
  active: boolean;
  reduced: boolean;
  composer: RefObject<HTMLElement | null>;
  destination: RefObject<HTMLElement | null>;
  arrival: RefObject<HTMLElement | null>;
  onComplete: () => void;
};

const duration = 4200;
const ease = 'cubic-bezier(.42,0,.28,1)';
const mix = (a: number, b: number, t: number) => a + (b - a) * t;
const px = (value: number) => `${value}px`;

export function usePaymentTransfer({ active, reduced, composer, destination, arrival, onComplete }: Props) {
  const complete = useRef(onComplete);
  const slot = useRef<{ element: HTMLElement; height: number; previous: string } | null>(null);
  complete.current = onComplete;
  useLayoutEffect(() => {
    if (active || !slot.current) return;
    const held = slot.current;
    slot.current = null;
    const source = composer.current;
    const restore = () => {
      if (slot.current?.element !== held.element) held.element.style.height = held.previous;
    };
    if (!source || reduced || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      restore();
      return;
    }
    const height = parseFloat(getComputedStyle(source).height) || source.offsetHeight;
    held.element.style.height = px(height);
    let animation: Animation;
    try {
      animation = held.element.animate([{ height: px(held.height) }, { height: px(height) }], {
        duration: 800,
        easing: ease,
        fill: 'both',
      });
    } catch {
      restore();
      return;
    }
    void animation.finished.then(() => {
      animation.cancel();
      restore();
    }, restore);
    return () => {
      animation.cancel();
      restore();
    };
  }, [active, composer, reduced]);
  useLayoutEffect(() => {
    if (!active) return;
    const source = composer.current,
      target = arrival.current;
    const motionPreference = window.matchMedia('(prefers-reduced-motion: reduce)');
    if (!source || !target || reduced || motionPreference.matches) {
      complete.current();
      return;
    }
    let stopped = false;
    let completed = false;
    const animations: Animation[] = [];
    const overlay = document.createElement('div');
    overlay.className = 'payment-transfer-layer';
    overlay.setAttribute('aria-hidden', 'true');
    overlay.inert = true;
    const world = document.createElement('div');
    world.className = 'payment-transfer-world';
    overlay.append(world);
    const previousVisibility = source.style.visibility;
    const startScroll = { x: scrollX, y: scrollY };
    const followScroll = () => {
      world.style.transform = `translate(${px(startScroll.x - scrollX)},${px(startScroll.y - scrollY)})`;
    };
    const settle = () => {
      if (!stopped && !completed) {
        completed = true;
        complete.current();
      }
    };
    const preferenceChanged = () => {
      if (motionPreference.matches) settle();
    };
    const animate = (element: HTMLElement, frames: Keyframe[]) => {
      const animation = element.animate(frames, { duration, easing: ease, fill: 'forwards' });
      animations.push(animation);
      return animation;
    };
    const travel = async () => {
      try {
        // Measure the actual, shared receipt markup without its final two-degree rotation.
        // Both papers then use the same origin; no transformed bounding-box approximation.
        const from = source.getBoundingClientRect();
        // In a vertical layout, replacing the form must not move the destination
        // during the handoff. Its height is released smoothly with the fresh card.
        const holder = source.parentElement;
        if (holder) {
          slot.current = { element: holder, height: from.height, previous: holder.style.height };
          holder.style.height = px(from.height);
        }
        const previousTransform = target.style.transform;
        target.style.transform = 'none';
        const to = target.getBoundingClientRect();
        const sourceAmount = source.querySelector<HTMLInputElement>('.r-money input');
        const targetAmount = target.querySelector<HTMLElement>('.r-receipt-amount');
        const sourceCurrency = source.querySelector<HTMLElement>('.r-money > span');
        if (!sourceAmount || !targetAmount || !sourceCurrency) {
          target.style.transform = previousTransform;
          settle();
          return;
        }
        const sourceAmountBox = sourceAmount.getBoundingClientRect();
        const targetAmountBox = targetAmount.getBoundingClientRect();
        const sourceCurrencyBox = sourceCurrency.getBoundingClientRect();
        const sourceType = getComputedStyle(sourceAmount);
        const targetType = getComputedStyle(targetAmount);
        const currencyType = getComputedStyle(sourceCurrency);
        const range = document.createRange();
        const currencyNode = targetAmount.firstChild;
        if (!currencyNode || currencyNode.nodeType !== Node.TEXT_NODE)
          throw new Error('Receipt amount missing');
        range.setStart(currencyNode, 0);
        range.setEnd(currencyNode, 1);
        const currencyWidth = range.getBoundingClientRect().width;
        const sourceNote = source.querySelector<HTMLTextAreaElement>('.r-message');
        const targetNote = target.querySelector<HTMLElement>('.r-note');
        const sourceNoteBox = sourceNote?.getBoundingClientRect();
        const targetNoteBox = targetNote?.getBoundingClientRect();
        const sourceStyle = getComputedStyle(source);
        const targetStyle = getComputedStyle(target);
        const paper = source.cloneNode(false) as HTMLElement;
        paper.className = 'r-ticket payment-transfer-paper';
        Object.assign(paper.style, {
          position: 'absolute',
          left: px(from.left),
          top: px(from.top),
          margin: '0',
          padding: '0',
          width: px(from.width),
          height: px(from.height),
          minHeight: '0',
          visibility: 'visible',
          transform: 'none',
          transformOrigin: '18% 2%',
        });
        const form = document.createElement('div');
        form.className = 'payment-transfer-compose';
        Object.assign(form.style, {
          width: px(from.width),
          height: px(from.height),
          padding: sourceStyle.padding,
        });
        for (const child of source.children) form.append(child.cloneNode(true));
        // This layer is outside receipt-form's container query. Preserve its resolved
        // columns and spans so the controls cannot reflow on the very first frame.
        const presets = source.querySelector<HTMLElement>('.r-presets');
        const clonedPresets = form.querySelector<HTMLElement>('.r-presets');
        if (presets && clonedPresets) {
          clonedPresets.style.gridTemplateColumns = getComputedStyle(presets).gridTemplateColumns;
          for (let index = 0; index < presets.children.length; index++) {
            const clone = clonedPresets.children[index] as HTMLElement;
            clone.style.gridColumn = getComputedStyle(presets.children[index]).gridColumn;
          }
        }
        // The one shared amount and message remain legible throughout; only controls crossfade.
        for (const el of form.querySelectorAll<HTMLElement>('.r-money, .r-message-field'))
          el.style.visibility = 'hidden';
        const receipt = target.cloneNode(true) as HTMLElement;
        receipt.className = 'r-ticket r-featured payment-transfer-receipt';
        Object.assign(receipt.style, {
          visibility: 'visible',
          transform: 'none',
          boxShadow: 'none',
          background: 'transparent',
          padding: targetStyle.padding,
        });
        for (const el of receipt.querySelectorAll<HTMLElement>('.r-receipt-amount, .r-note'))
          el.style.visibility = 'hidden';
        for (const el of [paper, ...form.querySelectorAll('[id]'), ...receipt.querySelectorAll('[id]')])
          el.removeAttribute('id');
        paper.append(form, receipt);
        const sharedText = (text: string, type: CSSStyleDeclaration) => {
          const element = document.createElement('div');
          element.className = 'payment-transfer-text';
          element.textContent = text;
          Object.assign(element.style, {
            fontFamily: type.fontFamily,
            fontWeight: type.fontWeight,
            lineHeight: type.lineHeight,
            color: type.color,
            letterSpacing: type.letterSpacing,
          });
          paper.append(element);
          return element;
        };
        const amount = sharedText(sourceAmount.value, sourceType);
        const currency = sharedText('¥', sourceType);
        world.append(paper);
        (source.closest('.d-app') ?? source.parentElement ?? document.body).append(overlay);
        const frames: Keyframe[] = [],
          amountFrames: Keyframe[] = [],
          currencyFrames: Keyframe[] = [];
        const dx = to.left - from.left,
          dy = to.top - from.top;
        const narrow = Math.abs(dx) < from.width / 2;
        for (let step = 0; step <= 60; step++) {
          const t = step / 60,
            u = 1 - t;
          const raw = Math.max(0, Math.min(1, (t - 0.08) / 0.82));
          const morph = raw * raw * (3 - 2 * raw);
          // A gentle bend of the whole sheet, never a tiny, fading flying token.
          const x =
            3 * u * u * t * (narrow ? -14 : dx * 0.17) +
            3 * u * t * t * (narrow ? 16 : dx * 0.78) +
            t * t * t * dx;
          const y =
            3 * u * u * t * (narrow ? dy * 0.18 : -56) +
            3 * u * t * t * (narrow ? dy * 0.72 : dy - 42) +
            t * t * t * dy;
          frames.push({
            offset: t,
            width: px(mix(from.width, to.width, morph)),
            height: px(mix(from.height, to.height, morph)),
            transform:
              'translate(' +
              px(x) +
              ',' +
              px(y) +
              ') rotate(' +
              (-4 * Math.sin(Math.PI * t) + 2 * t) +
              'deg) rotateX(' +
              7 * Math.sin(Math.PI * t) +
              'deg)',
            opacity: 1,
          });
          amountFrames.push({
            offset: t,
            left: px(
              mix(sourceAmountBox.left - from.left, targetAmountBox.left - to.left + currencyWidth, morph),
            ),
            top: px(mix(sourceAmountBox.top - from.top, targetAmountBox.top - to.top, morph)),
            fontSize: px(mix(parseFloat(sourceType.fontSize), parseFloat(targetType.fontSize), morph)),
            letterSpacing: px(
              mix(
                parseFloat(sourceType.letterSpacing) || 0,
                parseFloat(targetType.letterSpacing) || 0,
                morph,
              ),
            ),
            lineHeight: px(mix(parseFloat(sourceType.lineHeight), parseFloat(targetType.lineHeight), morph)),
          });
          currencyFrames.push({
            offset: t,
            left: px(mix(sourceCurrencyBox.left - from.left, targetAmountBox.left - to.left, morph)),
            top: px(mix(sourceCurrencyBox.top - from.top, targetAmountBox.top - to.top, morph)),
            fontSize: px(mix(parseFloat(currencyType.fontSize), parseFloat(targetType.fontSize), morph)),
            lineHeight: px(
              mix(parseFloat(currencyType.lineHeight), parseFloat(targetType.lineHeight), morph),
            ),
            letterSpacing: px(
              mix(
                parseFloat(currencyType.letterSpacing) || 0,
                parseFloat(targetType.letterSpacing) || 0,
                morph,
              ),
            ),
          });
        }
        // Restore measurement styles before the first paint; the hidden arrival is never shown.
        target.style.transform = previousTransform;
        source.style.visibility = 'hidden';
        window.addEventListener('scroll', followScroll, { passive: true });
        window.addEventListener('resize', settle, { once: true });
        motionPreference.addEventListener('change', preferenceChanged);
        animate(paper, frames);
        animate(form, [{ opacity: 1 }, { opacity: 0, offset: 0.3 }, { opacity: 0 }]);
        animate(receipt, [
          { opacity: 0 },
          { opacity: 0, offset: 0.3 },
          { opacity: 1, offset: 0.85 },
          { opacity: 1 },
        ]);
        animate(amount, amountFrames);
        animate(currency, currencyFrames);
        animate(currency, [
          { color: currencyType.color, fontWeight: currencyType.fontWeight },
          { color: targetType.color, fontWeight: targetType.fontWeight },
        ]);
        if (sourceNote && targetNote && sourceNoteBox && targetNoteBox) {
          const noteType = getComputedStyle(sourceNote),
            endType = getComputedStyle(targetNote);
          const note = sharedText(targetNote.textContent ?? '', noteType);
          note.classList.add('payment-transfer-note');
          animate(note, [
            {
              left: px(sourceNoteBox.left - from.left + parseFloat(noteType.paddingLeft)),
              top: px(sourceNoteBox.top - from.top + parseFloat(noteType.paddingTop)),
              width: px(
                sourceNoteBox.width - parseFloat(noteType.paddingLeft) - parseFloat(noteType.paddingRight),
              ),
              height: px(sourceNoteBox.height),
              fontSize: noteType.fontSize,
              lineHeight: noteType.lineHeight,
            },
            {
              left: px(targetNoteBox.left - to.left),
              top: px(targetNoteBox.top - to.top),
              width: px(targetNoteBox.width),
              height: px(targetNoteBox.height),
              fontSize: endType.fontSize,
              lineHeight: endType.lineHeight,
            },
          ]);
        }
        if (narrow && (to.bottom > innerHeight || to.top < 80))
          destination.current?.scrollIntoView({ behavior: 'smooth', block: 'center' });
        await Promise.all(animations.map((animation) => animation.finished));
        // Keep the exact final frame until React installs the identical real receipt.
        await new Promise(requestAnimationFrame);
        settle();
      } catch {
        // A resize, navigation or unsupported animation must not trap a confirmed payment.
        settle();
      }
    };
    void travel();
    return () => {
      stopped = true;
      for (const animation of animations) animation.cancel();
      overlay.remove();
      source.style.visibility = previousVisibility;
      if (!completed && slot.current) {
        slot.current.element.style.height = slot.current.previous;
        slot.current = null;
      }
      window.removeEventListener('scroll', followScroll);
      window.removeEventListener('resize', settle);
      motionPreference.removeEventListener('change', preferenceChanged);
    };
  }, [active, reduced, composer, destination, arrival]);
}
