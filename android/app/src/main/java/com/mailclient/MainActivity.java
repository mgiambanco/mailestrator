package com.mailclient;

import android.Manifest;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Bundle;
import android.view.MenuItem;

import androidx.annotation.NonNull;
import androidx.appcompat.app.ActionBarDrawerToggle;
import androidx.appcompat.app.AppCompatActivity;
import androidx.appcompat.widget.Toolbar;
import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;
import androidx.drawerlayout.widget.DrawerLayout;
import androidx.lifecycle.ViewModelProvider;

import com.mailclient.data.model.Account;
import com.mailclient.ui.inbox.InboxFragment;
import com.mailclient.ui.sidebar.SidebarFragment;
import com.mailclient.ui.viewmodel.MailViewModel;

/**
 * Single activity. Hosts a DrawerLayout: left drawer = SidebarFragment (accounts),
 * main content = InboxFragment or EmailDetailFragment.
 */
public class MainActivity extends AppCompatActivity {

    private static final int REQUEST_NOTIFICATION_PERMISSION = 1;

    DrawerLayout drawerLayout;
    MailViewModel viewModel;

    private final BroadcastReceiver fcmTokenReceiver = new BroadcastReceiver() {
        @Override public void onReceive(Context ctx, Intent intent) {
            String token = intent.getStringExtra("token");
            if (token != null) viewModel.setFcmToken(token);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);

        Toolbar toolbar = findViewById(R.id.toolbar);
        setSupportActionBar(toolbar);

        drawerLayout = findViewById(R.id.drawer_layout);
        ActionBarDrawerToggle toggle = new ActionBarDrawerToggle(
                this, drawerLayout, toolbar,
                R.string.nav_open, R.string.nav_close);
        drawerLayout.addDrawerListener(toggle);
        toggle.syncState();

        viewModel = new ViewModelProvider(this).get(MailViewModel.class);

        // Load accounts in sidebar
        if (savedInstanceState == null) {
            getSupportFragmentManager()
                    .beginTransaction()
                    .replace(R.id.sidebar_container, new SidebarFragment())
                    .commit();
            getSupportFragmentManager()
                    .beginTransaction()
                    .replace(R.id.fragment_container, new InboxFragment())
                    .commit();
        }

        // When account selected, close drawer and load emails
        viewModel.selectedAccount.observe(this, account -> {
            if (account != null) {
                drawerLayout.closeDrawers();
                viewModel.loadEmails(account.id);
                if (getSupportActionBar() != null) {
                    getSupportActionBar().setTitle(account.getDisplayName());
                }
            }
        });

        // Handle notification tap deep-link
        handleIntent(getIntent().getStringExtra("account_id"),
                     getIntent().getStringExtra("email_id"));

        requestNotificationPermission();

        // Connect WebSockets and register FCM token
        viewModel.connectAllWebSockets();

        // Listen for FCM token refreshes from MailFcmService
        IntentFilter filter = new IntentFilter("com.mailclient.FCM_TOKEN_UPDATED");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(fcmTokenReceiver, filter, Context.RECEIVER_NOT_EXPORTED);
        } else {
            registerReceiver(fcmTokenReceiver, filter);
        }

        // Fetch the current FCM token on startup
        com.google.firebase.messaging.FirebaseMessaging.getInstance().getToken()
                .addOnSuccessListener(token -> viewModel.setFcmToken(token));
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        unregisterReceiver(fcmTokenReceiver);
    }

    @Override
    protected void onNewIntent(android.content.Intent intent) {
        super.onNewIntent(intent);
        handleIntent(intent.getStringExtra("account_id"),
                     intent.getStringExtra("email_id"));
    }

    private void handleIntent(String accountId, String emailId) {
        if (accountId == null || emailId == null) return;
        viewModel.accounts.observe(this, accounts -> {
            for (Account a : accounts) {
                if (a.id.equals(accountId)) {
                    viewModel.selectedAccount.setValue(a);
                    // Load the specific email
                    com.mailclient.network.ApiClient.getInstance().getEmail(
                            accountId, emailId, a.token,
                            new com.mailclient.network.ApiCallback<com.mailclient.data.model.Email>() {
                                @Override public void onSuccess(com.mailclient.data.model.Email email) {
                                    runOnUiThread(() -> {
                                        viewModel.selectedEmail.setValue(email);
                                        navigateToDetail();
                                    });
                                }
                                @Override public void onFailure(String e) {}
                            });
                    break;
                }
            }
        });
    }

    public void navigateToDetail() {
        getSupportFragmentManager()
                .beginTransaction()
                .replace(R.id.fragment_container,
                        new com.mailclient.ui.detail.EmailDetailFragment())
                .addToBackStack(null)
                .commit();
    }

    public void closeDrawer() {
        drawerLayout.closeDrawers();
    }

    private void requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                    != PackageManager.PERMISSION_GRANTED) {
                ActivityCompat.requestPermissions(this,
                        new String[]{Manifest.permission.POST_NOTIFICATIONS},
                        REQUEST_NOTIFICATION_PERMISSION);
            }
        }
    }
}
