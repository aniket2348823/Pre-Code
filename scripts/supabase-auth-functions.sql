-- VigilAgent Custom Auth for Supabase RLS
-- This creates a session-based auth system that works with VigilAgent's own JWT.
-- Run BEFORE the RLS policies.

-- =====================================================
-- Schema for custom auth
-- =====================================================
CREATE SCHEMA IF NOT EXISTS app_auth;

-- Set the current user ID for the session (called by Go middleware after JWT validation)
CREATE OR REPLACE FUNCTION app_auth.set_current_user_id(user_id UUID)
RETURNS VOID AS $$
BEGIN
    PERFORM set_config('app.current_user_id', user_id::text, true);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Get the current user ID from the session (used by RLS policies)
CREATE OR REPLACE FUNCTION app_auth.current_user_id()
RETURNS UUID AS $$
DECLARE
    uid TEXT;
BEGIN
    uid := current_setting('app.current_user_id', true);
    IF uid IS NULL OR uid = '' THEN
        RETURN NULL;
    END IF;
    RETURN uid::UUID;
END;
$$ LANGUAGE plpgsql STABLE;

-- Convenience function to check org membership (used by RLS policies)
CREATE OR REPLACE FUNCTION app_auth.is_org_member(org_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM organization_members
        WHERE organization_id = org_id AND user_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Convenience function to check if user is org owner
CREATE OR REPLACE FUNCTION app_auth.is_org_owner(org_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM organizations
        WHERE id = org_id AND owner_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Check if user is admin
CREATE OR REPLACE FUNCTION app_auth.is_admin()
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM users
        WHERE id = app_auth.current_user_id() AND role IN ('admin', 'superadmin')
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Check if user owns a resource (by user_id column)
CREATE OR REPLACE FUNCTION app_auth.owns_resource(owner UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN app_auth.current_user_id() = owner;
END;
$$ LANGUAGE plpgsql STABLE;

-- Check if user can access a project (through org membership)
CREATE OR REPLACE FUNCTION app_auth.can_access_project(project_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM projects p
        JOIN organization_members om ON om.organization_id = p.org_id
        WHERE p.id = project_id AND om.user_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Check if user can access a session (through project -> org membership)
CREATE OR REPLACE FUNCTION app_auth.can_access_session(sess_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM sessions s
        JOIN projects p ON p.id = s.project_id
        JOIN organization_members om ON om.organization_id = p.org_id
        WHERE s.id = sess_id AND om.user_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- =====================================================
-- DONE! Now run the RLS policies that use these functions
-- =====================================================
