import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { catchError, map, of } from 'rxjs';
import { AuthService } from './auth.service';

export const dashboardGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  const router = inject(Router);
  return auth.hasSession() ? true : router.createUrlTree(['/login']);
};

export const roleGuard: CanActivateFn = (route) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  if (!auth.hasSession()) return router.createUrlTree(['/login']);

  const roles = route.data['roles'] as string[] | undefined;
  return auth.currentUser().pipe(
    map(({ user }) => roles?.includes(user.role) ? true : router.createUrlTree([defaultRoute(user.role)])),
    catchError(() => {
      auth.clearStoredSession();
      return of(router.createUrlTree(['/login']));
    }),
  );
};

function defaultRoute(role: string): string {
  if (role === 'SUPPORT') return '/assigned';
  if (role === 'USER') return '/my-tickets';
  return '/dashboard';
}
